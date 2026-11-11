package coding

import (
	"context"
	"sync"
	"testing"

	"github.com/dat267/pier/ai"
)

// The interactive /reload runs Reload off the UI loop, while the loop reads the
// prompt templates and diagnostics (autocomplete, the loaded-resources view)
// and prompts expand templates. Upstream reload (agent-session.ts, pi v1.5.2
// b4afdddf) runs on the one event loop; the port guards the replaced resources
// so the race detector finds no unsynchronized access.
func TestReloadWhileResourcesAreReadIsRaceFree(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: stubStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	var group sync.WaitGroup
	group.Go(func() {
		for range 10 {
			session.Reload()
		}
	})
	group.Go(func() {
		for range 10 {
			_ = session.Prompt(context.Background(), "hello", &PromptOptions{StreamingBehavior: "followUp"})
		}
	})
	group.Go(func() {
		for range 500 {
			_ = session.PromptTemplates()
			_ = session.PromptDiagnostics()
			_ = session.SkillDiagnostics()
		}
	})
	group.Wait()
}
