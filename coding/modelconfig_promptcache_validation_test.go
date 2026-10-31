package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pi v1.4.0 core/model-config.ts validates promptCache with
// ModelPromptCacheSchema: an object whose tier values are numbers above zero
// (Type.Number exclusiveMinimum 0). Extra tier keys are allowed.
func TestLoadModelConfigValidatesPromptCache(t *testing.T) {
	cases := []struct {
		name     string
		member   string
		wantErr  bool
		wantPath string
	}{
		{"zero tier", `"promptCache": {"short": 0}`, true, "promptCache.short"},
		{"negative tier", `"promptCache": {"long": -1}`, true, "promptCache.long"},
		{"string tier", `"promptCache": {"short": "3600"}`, true, "promptCache.short"},
		{"array", `"promptCache": [1]`, true, "promptCache"},
		{"null", `"promptCache": null`, true, "promptCache"},
		{"fractional tier control", `"promptCache": {"short": 1.5}`, false, ""},
		{"extra tier key control", `"promptCache": {"bogus": 5}`, false, ""},
		{"empty object control", `"promptCache": {}`, false, ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "models.json")
			content := `{"providers": {"custom": {"models": [{"id": "m", ` + test.member + `}]}}}`
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			message := LoadModelConfig(path).GetError()
			if test.wantErr {
				if message == "" {
					t.Fatalf("config loaded without an error for %s", test.member)
				}
				if !strings.Contains(message, test.wantPath) {
					t.Fatalf("error = %q, want it to name %s", message, test.wantPath)
				}
				return
			}
			if message != "" {
				t.Fatalf("control rejected: %s", message)
			}
		})
	}
}
