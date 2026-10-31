package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pi v1.4.0 core/model-config.ts validates models.json with the typebox
// schema: contextWindow and maxTokens are numbers above zero (Type.Number
// exclusiveMinimum 0), every inputLimits counter is an integer of at least one,
// and jpegQuality is an integer from 1 to 100. Upstream rejects the whole file
// ("Invalid models.json schema") for values outside those bounds.
func TestLoadModelConfigEnforcesNumericRange(t *testing.T) {
	cases := []struct {
		name     string
		model    string
		wantErr  bool
		wantPath string
	}{
		{"contextWindow zero", `"contextWindow": 0`, true, "contextWindow"},
		{"contextWindow negative", `"contextWindow": -5`, true, "contextWindow"},
		{"contextWindow fractional control", `"contextWindow": 1.5`, false, ""},
		{"maxTokens zero", `"maxTokens": 0`, true, "maxTokens"},
		{"maxRequestBytes zero", `"inputLimits": {"maxRequestBytes": 0}`, true, "maxRequestBytes"},
		{"maxPerMessage negative", `"inputLimits": {"images": {"maxPerMessage": -1}}`, true, "maxPerMessage"},
		{"maxPerRequest fractional", `"inputLimits": {"images": {"maxPerRequest": 1.5}}`, true, "maxPerRequest"},
		{"resize maxWidth zero", `"inputLimits": {"images": {"resize": {"maxWidth": 0}}}`, true, "maxWidth"},
		{"resize maxBytes fractional", `"inputLimits": {"images": {"resize": {"maxBytes": 1.5}}}`, true, "maxBytes"},
		{"jpegQuality zero", `"inputLimits": {"images": {"resize": {"jpegQuality": 0}}}`, true, "jpegQuality"},
		{"jpegQuality above 100", `"inputLimits": {"images": {"resize": {"jpegQuality": 500}}}`, true, "jpegQuality"},
		{"jpegQuality 80 control", `"inputLimits": {"images": {"resize": {"jpegQuality": 80}}}`, false, ""},
		{
			"limits at their bounds control",
			`"inputLimits": {"maxRequestBytes": 1, "images": {"maxPerMessage": 1, "maxPerRequest": 1, ` +
				`"resize": {"maxWidth": 1, "maxHeight": 1, "maxBytes": 1, "jpegQuality": 100}}}`,
			false, "",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "models.json")
			content := `{"providers": {"custom": {"models": [{"id": "m", ` + test.model + `}]}}}`
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			message := LoadModelConfig(path).GetError()
			if test.wantErr {
				if message == "" {
					t.Fatalf("config loaded without an error for %s", test.model)
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
