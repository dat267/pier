package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream interpolates user text into "${text}" template literals, so a quote
// or backslash is copied rather than Go-escaped as %q would (pi v1.5.0
// core/model-resolver.ts resolveModelScopeFromModels, core/mcp-servers.ts).
func TestUserTextInterpolatesLikeTemplateLiterals(t *testing.T) {
	models := []*ai.Model{{ID: "gpt", Provider: "openai"}}
	result := ResolveModelScopeFromModels([]string{`a"b\c`}, models)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != `No models match pattern "a"b\c"` {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}

	_, err := validateMcpServerConfig("tools", map[string]any{
		"command":      "x",
		"toolExposure": map[string]any{`say "hi"`: "nope"},
	}, []string{`say "hi"`})
	want := `server "tools": toolExposure "say "hi"" must be one of "codemode", "deferred", "direct", "hidden"`
	if err == nil || err.Error() != want {
		t.Fatalf("toolExposure error = %v\nwant %s", err, want)
	}
}
