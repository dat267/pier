package coding

import (
	"github.com/dat267/pier/ai"
	"sync"
	"testing"
)

// TestProcessTreeGuardKillIsConcurrent pins that the guard's kill is safe to
// call from several goroutines at once: the shell tool's abort goroutine and its
// timeout timer can fire together, and both call Kill. The guard must run the
// platform kill once with no unsynchronized state (D178).
func TestProcessTreeGuardKillIsConcurrent(t *testing.T) {
	guard := newProcessTreeGuard(1 << 30) // a pid that cannot exist
	defer guard.Release()
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			guard.Kill()
		})
	}
	wg.Wait()
}

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
