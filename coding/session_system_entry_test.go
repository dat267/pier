package coding

import (
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// A new session persists its leading system message only when it declares tools:
// a toolless prompt carries no tool loadout to restore (D237), and an empty
// entry would be written otherwise.
func TestNewSessionPersistsTheSystemEntryOnlyWithTools(t *testing.T) {
	tempAgentDir(t)
	build := func(tools []agent.AgentTool) int {
		manager := NewSessionManager(t.TempDir(), nil)
		session, err := NewAgentSession(&SessionConfig{
			Cwd: t.TempDir(), Sessions: manager, StreamFn: stubStreamFn, Tools: tools,
			Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Dispose()
		return len(manager.GetEntries())
	}
	if got := build(nil); got != 0 {
		t.Fatalf("entries without tools = %d, want none", got)
	}
	if got := build([]agent.AgentTool{testTool("read")}); got != 1 {
		t.Fatalf("entries with tools = %d, want the system entry", got)
	}
}
