package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// TestSettingsGettersUpstreamParity pins each setting's default and the value a
// settings document produces against pi v1.5.2 (b4afdddf),
// core/settings-manager.ts. Regenerate from the repository root:
//
//	node --experimental-strip-types coding/interactive/testdata/gen-settingsgetters-parity.mjs ./pi > coding/interactive/testdata/settingsgetters_parity_golden.txt
//
// The corpus covers the built-in defaults, a full global document, a project
// document with and without project trust, invalid values for several keys and
// a partial global document.
func TestSettingsGettersUpstreamParity(t *testing.T) {
	var corpus struct {
		Cases map[string]struct {
			Global         json.RawMessage `json:"global"`
			Project        json.RawMessage `json:"project"`
			ProjectTrusted bool            `json:"projectTrusted"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/settingsgetters_corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}

	// The editor getter falls back to $VISUAL/$EDITOR; TestMain pins them to a
	// stub, so clear them to compare the built-in default.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	golden := loadGoldenFile(t, "testdata/settingsgetters_parity_golden.txt")

	// The reference names each getter getX; the port exports GetX, and the
	// two-result HTTP timeout getter is read separately below.
	getters := []string{
		"HideThinkingBlock", "ShowCacheMissNotices", "CollapseChangelog", "EnableAnalytics",
		"TrackingID", "EnableInstallTelemetry", "EnableSkillCommands", "NpmCommand", "ShellPath",
		"ShellCommandPrefix", "DefaultProjectTrust", "ThemeSetting", "Theme", "DefaultProvider",
		"DefaultModel", "DefaultThinkingLevel", "SteeringMode", "FollowUpMode", "Transport",
		"CompactionEnabled", "BranchSummarySkipPrompt", "RetryEnabled", "CacheWarmingMode",
		"ExternalEditorCommand", "ShowImages", "ImageWidthCells", "ClearOnShrink",
		"ShowTerminalProgress", "TuiMode", "FullscreenExitOutput", "FullscreenScrollbar",
		"FullscreenCopyOnSelect", "FullscreenWheelScrollLines", "ImageAutoResize", "BlockImages",
		"DoubleEscapeAction", "TreeFilterMode", "ShowHardwareCursor", "EditorPaddingX", "OutputPad",
		"AutocompleteMaxVisible", "CodeBlockIndent", "MermaidRenderingMode", "LastChangelogVersion",
		"SessionDir",
	}

	for _, label := range sortedKeys(corpus.Cases) {
		spec := corpus.Cases[label]
		dir := t.TempDir()
		cwd := filepath.Join(dir, "project")
		agentDir := filepath.Join(dir, "agent")
		if err := os.MkdirAll(filepath.Join(cwd, ".pi"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if len(spec.Global) > 0 && string(spec.Global) != "null" {
			if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), spec.Global, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if len(spec.Project) > 0 && string(spec.Project) != "null" {
			if err := os.WriteFile(filepath.Join(cwd, ".pi", "settings.json"), spec.Project, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		trusted := spec.ProjectTrusted
		manager := coding.NewSettingsManagerFromFiles(cwd, agentDir, coding.SettingsManagerCreateOptions{
			ProjectTrusted: &trusted,
		})

		values := make([]any, 0, len(getters)+1)
		reflected := reflect.ValueOf(manager)
		for _, name := range getters {
			method := reflected.MethodByName("Get" + name)
			if !method.IsValid() {
				t.Fatalf("%s: no getter Get%s", label, name)
			}
			out := method.Call(nil)[0].Interface()
			encoded, err := ai.MarshalJSON(out)
			if err != nil {
				t.Fatalf("%s: marshal Get%s: %v", label, name, err)
			}
			values = append(values, json.RawMessage(encoded))
		}
		timeout, err := manager.GetHTTPIdleTimeoutMS()
		if err != nil {
			t.Fatalf("%s: GetHTTPIdleTimeoutMS: %v", label, err)
		}
		values = append(values, timeout)

		checkGolden(t, golden, label, values)
	}
}
