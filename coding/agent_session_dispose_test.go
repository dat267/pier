package coding

import (
	"context"
	"testing"
	"time"

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
	if !session.isDisposed() {
		t.Fatal("the session was not marked disposed")
	}
}

func unusedSessionStreamFn(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	panic("the dispose test never streams")
}

// Upstream dispose calls _disconnectFromAgent right after the aborts, so the
// events the aborted run emits later (its aborted assistant message, agent_end)
// never reach the disposed session: nothing more is persisted for it.
func TestDisposedSessionIgnoresTheAbortedRunsLateEvents(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	started := make(chan struct{})
	finish := make(chan struct{})
	streamFn := func(_ *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		partial := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", StopReason: ai.StopPending}
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: partial})
		close(started)
		go func() {
			<-options.Ctx.Done()
			<-finish // the abort lands after Dispose returns
			aborted := *partial
			aborted.StopReason = ai.StopAborted
			message := ai.RequestAbortedMessage
			aborted.ErrorMessage = &message
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: &aborted})
		}()
		return stream
	}
	session := newPromptSession(t, model, streamFn)
	promptDone := make(chan struct{})
	go func() {
		defer close(promptDone)
		_ = session.Prompt(context.Background(), "hello", nil)
	}()
	<-started
	before := len(session.Sessions.GetEntries())

	session.Dispose()
	close(finish)
	select {
	case <-promptDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the prompt did not return after Dispose")
	}
	if err := session.Agent.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after := len(session.Sessions.GetEntries()); after != before {
		t.Fatalf("the disposed session persisted %d more entries from the aborted run", after-before)
	}
}
