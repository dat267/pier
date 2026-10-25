package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type settingsLoadGetters struct {
	SteeringMode            string              `json:"steeringMode"`
	Theme                   *string             `json:"theme"`
	Transport               string              `json:"transport"`
	DefaultTools            []string            `json:"defaultTools"`
	EditorPaddingX          int                 `json:"editorPaddingX"`
	CompactionReserveTokens int64               `json:"compactionReserveTokens"`
	Retry                   RetrySettingsResult `json:"retry"`
}

// TestSettingsLoadMatchesUpstream pins the effective settings after loading
// global/project files: a member whose JSON type the typed settings cannot hold
// must only affect that member, not discard the rest of the file.
// Regenerate with
// node coding/testdata/settings-load/generate.mjs ./pi > coding/testdata/settings-load/golden.json
func TestSettingsLoadMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/settings-load/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]struct {
		Getters settingsLoadGetters `json:"getters"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	// The generator records the inputs beside the results only in its own case
	// table, so the test keeps the pinned inputs here.
	inputs := map[string]struct {
		global  string
		project string
	}{
		"valid-baseline":                   {global: `{"theme":"dark","steeringMode":"all","transport":"websocket","editorPaddingX":3,"compaction":{"enabled":false,"reserveTokens":12000},"retry":{"maxRetries":5,"baseDelayMs":250},"defaultTools":["read","bash"]}`},
		"global-theme-number":              {global: `{"theme":5,"steeringMode":"all","transport":"websocket","editorPaddingX":3,"compaction":{"enabled":false,"reserveTokens":12000},"retry":{"maxRetries":5,"baseDelayMs":250},"defaultTools":["read","bash"]}`},
		"global-theme-array":               {global: `{"theme":["dark"],"steeringMode":"all","transport":"websocket","editorPaddingX":3,"compaction":{"enabled":false,"reserveTokens":12000},"retry":{"maxRetries":5,"baseDelayMs":250},"defaultTools":["read","bash"]}`},
		"global-compaction-enabled-string": {global: `{"theme":"dark","steeringMode":"all","transport":"websocket","editorPaddingX":3,"compaction":{"enabled":"yes","reserveTokens":12000},"retry":{"maxRetries":5,"baseDelayMs":250},"defaultTools":["read","bash"]}`},
		"project-theme-number": {
			global:  `{"theme":"dark","steeringMode":"one-at-a-time","transport":"websocket","editorPaddingX":3,"compaction":{"enabled":false,"reserveTokens":12000},"retry":{"maxRetries":5,"baseDelayMs":250},"defaultTools":["read","bash"]}`,
			project: `{"theme":5,"steeringMode":"all","editorPaddingX":7}`,
		},
		"project-compaction-enabled-string": {
			global:  `{"theme":"dark","steeringMode":"all","transport":"websocket","editorPaddingX":3,"compaction":{"enabled":false,"reserveTokens":12000},"retry":{"maxRetries":5,"baseDelayMs":250},"defaultTools":["read","bash"]}`,
			project: `{"compaction":{"enabled":"yes","keepRecentTokens":5000}}`,
		},
		"invalid-json": {global: `{"theme": "dark",`},
		"unknown-key":  {global: `{"theme":"dark","steeringMode":"all","transport":"websocket","editorPaddingX":3,"compaction":{"enabled":false,"reserveTokens":12000},"retry":{"maxRetries":5,"baseDelayMs":250},"defaultTools":["read","bash"],"xUnknown":{"a":1}}`},
	}
	if len(fixture) != len(inputs) {
		t.Fatalf("fixture cases = %d, inputs = %d", len(fixture), len(inputs))
	}
	for name, test := range inputs {
		t.Run(name, func(t *testing.T) {
			want, ok := fixture[name]
			if !ok {
				t.Fatalf("no fixture for %q", name)
			}
			cwd, agentDir := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(test.global), 0o600); err != nil {
				t.Fatal(err)
			}
			if test.project != "" {
				if err := os.MkdirAll(filepath.Join(cwd, ConfigDirName), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cwd, ConfigDirName, "settings.json"), []byte(test.project), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			trusted := true
			manager := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{ProjectTrusted: &trusted})
			theme := manager.GetThemeSetting()
			reserve, err := manager.GetCompactionReserveTokens(nil)
			if err != nil {
				t.Fatal(err)
			}
			got := settingsLoadGetters{
				SteeringMode:            manager.GetSteeringMode(),
				Theme:                   theme,
				Transport:               manager.GetTransport(),
				DefaultTools:            manager.GetDefaultTools(),
				EditorPaddingX:          manager.GetEditorPaddingX(),
				CompactionReserveTokens: reserve,
				Retry:                   manager.GetRetrySettings(),
			}
			if !reflect.DeepEqual(got, want.Getters) {
				t.Errorf("getters = %+v, want %+v", got, want.Getters)
			}
		})
	}
}
