package coding

import (
	"context"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// A /tools change made without a turn afterwards must still survive a relaunch.
// The agent loop records a tool-state diff only when a request runs, so a toggle
// followed by closing pier was lost; the session records it itself (D498).
func TestToolToggleWithoutATurnSurvivesResume(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000}
	newSession := func(usesDefaults bool) *AgentSession {
		session, err := NewAgentSession(&SessionConfig{
			Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
			Model: model, Tools: []agent.AgentTool{testTool("read"), testTool("bash"), testTool("edit")},
			UsesDefaultTools: usesDefaults,
		})
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	session := newSession(false)
	if err := session.SetToolEnabled("bash", false); err != nil {
		t.Fatal(err)
	}
	session.Dispose()

	resumed := newSession(true)
	defer resumed.Dispose()
	for _, name := range resumed.GetActiveToolNames() {
		if name == "bash" {
			t.Fatalf("resumed active tools = %v, want bash to stay disabled", resumed.GetActiveToolNames())
		}
	}
	if len(resumed.GetActiveToolNames()) != 2 {
		t.Fatalf("resumed active tools = %v, want read and edit", resumed.GetActiveToolNames())
	}

	// Re-enabling is recorded too, so the next resume has it back.
	if err := resumed.SetToolEnabled("bash", true); err != nil {
		t.Fatal(err)
	}
	resumed.Dispose()
	again := newSession(true)
	defer again.Dispose()
	found := false
	for _, name := range again.GetActiveToolNames() {
		if name == "bash" {
			found = true
		}
	}
	if !found {
		t.Fatalf("active tools = %v, want bash back after re-enabling", again.GetActiveToolNames())
	}
	_ = context.Background
}
