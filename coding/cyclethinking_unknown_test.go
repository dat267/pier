package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.5.2 (b4afdddf) core/agent-session.ts cycleThinkingLevel computes
// `(levels.indexOf(level) + 1) % levels.length`, so a current level that is not
// in the supported list (index -1) advances to levels[0]. The port's scan left
// its index at 0 and returned levels[1].
func TestCycleThinkingLevelWithAnUnknownLevelStartsAtTheFirst(t *testing.T) {
	manager, _ := newTestSession(t)
	model := &ai.Model{
		Provider: "anthropic", ID: "claude-sonnet-4-5", API: ai.APIAnthropicMessages,
		ContextWindow: 200000, MaxTokens: 8192, Reasoning: true,
	}
	session, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Sessions: manager, StreamFn: stubStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	session.Agent.SetModel(model)
	session.Agent.SetThinkingLevel(ai.ThinkingLevel("not-a-supported-level"))
	levels := session.GetAvailableThinkingLevels()
	if len(levels) < 2 {
		t.Fatalf("supported levels = %v, want at least two", levels)
	}
	next, ok := session.CycleThinkingLevel(ModelMutationOptions{})
	if !ok || next != levels[0] {
		t.Fatalf("cycle from an unknown level = %q (ok=%v), want %q", next, ok, levels[0])
	}
}
