package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream agent-session.ts (pi v1.5.0 f9a26365) dispose wraps only the abort
// calls in try/catch ("Dispose must succeed even if an abort hook throws"); the
// listener reset, cache-warmer cancel and resource cleanup still run. A
// panicking abort (here a session without an agent) must not leave the
// session undisposed with its listeners attached.
func TestDisposeFinishesWhenAnAbortPanics(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := newPromptSession(t, model, unusedSessionStreamFn)
	session.Agent = nil // makes the abort panic
	session.Subscribe(func(*SessionEvent) {})
	session.Dispose()

	session.listenerMu.Lock()
	listeners := len(session.listeners)
	session.listenerMu.Unlock()
	if listeners != 0 {
		t.Fatalf("listeners after Dispose = %d, want 0", listeners)
	}
	session.control.stateMu.Lock()
	disposed := session.control.disposed
	session.control.stateMu.Unlock()
	if !disposed {
		t.Fatal("the session was not marked disposed")
	}
}

func unusedSessionStreamFn(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	panic("the dispose test never streams")
}
