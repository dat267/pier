package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// upperFirst turns a setting key into the setter's suffix (hideThinkingBlock ->
// HideThinkingBlock).
func upperFirst(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// TestSettingsSettersUpstreamParity pins the settings file a sequence of setters
// writes against pi v1.5.2 (b4afdddf), core/settings-manager.ts: field order,
// values dropped when set to undefined, and keys the initial document carried.
// Regenerate from the repository root:
//
//	node --experimental-strip-types coding/interactive/testdata/gen-settingssetters-parity.mjs ./pi > coding/interactive/testdata/settingssetters_parity_golden.txt
//
// The setters the reference has no counterpart for (externalEditor,
// codeBlockIndent, sessionDir, branchSummarySkipPrompt) are not in the corpus.
func TestSettingsSettersUpstreamParity(t *testing.T) {
	var corpus struct {
		Cases map[string]struct {
			Initial json.RawMessage  `json:"initial"`
			Ops     [][2]interface{} `json:"ops"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/settingssetters_corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}

	golden := loadGoldenFile(t, "testdata/settingssetters_parity_golden.txt")

	for _, label := range sortedKeys(corpus.Cases) {
		spec := corpus.Cases[label]
		dir := t.TempDir()
		cwd := filepath.Join(dir, "project")
		agentDir := filepath.Join(dir, "agent")
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		settingsPath := filepath.Join(agentDir, "settings.json")
		if len(spec.Initial) > 0 && string(spec.Initial) != "null" {
			if err := os.WriteFile(settingsPath, spec.Initial, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		trusted := true
		manager := coding.NewSettingsManagerFromFiles(cwd, agentDir, coding.SettingsManagerCreateOptions{
			ProjectTrusted: &trusted,
		})
		for _, op := range spec.Ops {
			name, ok := op[0].(string)
			if !ok {
				t.Fatalf("%s: op name %v", label, op[0])
			}
			callSetter(t, label, manager, name, op[1])
		}
		manager.FlushPersists()

		written, err := os.ReadFile(settingsPath)
		if err != nil {
			if os.IsNotExist(err) {
				checkGolden(t, golden, label, nil)
				continue
			}
			t.Fatalf("%s: read settings: %v", label, err)
		}
		checkGolden(t, golden, label, string(written))
	}
}

// callSetter invokes Set<Name> with the corpus value, converting it to the
// setter's parameter: a JSON null becomes a nil pointer, a number an int, an
// array a []string.
func callSetter(t *testing.T, label string, manager *coding.SettingsManager, name string, value any) {
	t.Helper()
	method := reflect.ValueOf(manager).MethodByName("Set" + upperFirst(name))
	if !method.IsValid() {
		t.Fatalf("%s: no setter Set%s", label, upperFirst(name))
	}
	param := method.Type().In(0)
	var argument reflect.Value
	switch param.Kind() {
	case reflect.Bool:
		argument = reflect.ValueOf(value.(bool))
	case reflect.String:
		argument = reflect.ValueOf(value.(string))
	case reflect.Int:
		argument = reflect.ValueOf(int(value.(float64)))
	case reflect.Slice:
		if value == nil {
			argument = reflect.Zero(param)
			break
		}
		items := []string{}
		if list, ok := value.([]any); ok {
			for _, item := range list {
				items = append(items, item.(string))
			}
		}
		argument = reflect.ValueOf(items)
	case reflect.Pointer:
		if value == nil {
			argument = reflect.Zero(param)
			break
		}
		pointer := reflect.New(param.Elem())
		switch param.Elem().Kind() {
		case reflect.String:
			pointer.Elem().SetString(value.(string))
		case reflect.Int:
			pointer.Elem().SetInt(int64(value.(float64)))
		default:
			t.Fatalf("%s: unsupported pointer setter %s", label, param)
		}
		argument = pointer
	default:
		t.Fatalf("%s: unsupported setter parameter %s", label, param)
	}
	method.Call([]reflect.Value{argument})
}
