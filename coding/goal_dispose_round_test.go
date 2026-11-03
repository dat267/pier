package coding

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
)

// D223's native goal scheduler must not launch deferred work after its session
// is disposed. Reference: pi v1.5.0 (f9a26365), coding-agent/src/core/
// agent-session.ts dispose and agent-session-runtime.ts teardownCurrent.
func TestDisposedSessionDoesNotStartAReservedGoalRound(t *testing.T) {
	manager, dir := newTestSession(t)
	calls := 0
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Goal: true, Control: &AgentSessionControl{},
		Model: &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128},
		StreamFn: func(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			calls++
			stream := ai.NewAssistantMessageEventStream()
			message := &ai.AssistantMessage{API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopError, ErrorMessage: new("bounded test response")}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopError, Message: message})
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var pending []func(context.Context) error
	session.Goal.SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
	session.Goal.HandleCommand("set queued before teardown")
	if len(pending) != 1 {
		t.Fatalf("reserved rounds = %d, want 1", len(pending))
	}
	session.Dispose()
	_ = pending[0](context.Background())
	if calls != 0 {
		t.Fatalf("model calls after disposal = %d, want 0", calls)
	}
}
