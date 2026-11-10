package coding

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// Disposing a session while prompts and aborts are in flight, from several
// goroutines, must let every call return and leave the session idle; a prompt
// after Dispose must not hang. Upstream agent-session.ts (pi v1.5.0 f9a26365)
// runs these on one event loop; here callers overlap.
func TestAgentSessionDisposeRacingPromptsSettles(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	streamFn := func(_ *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			select {
			case <-options.Ctx.Done():
				aborted := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", StopReason: ai.StopAborted}
				message := ai.RequestAbortedMessage
				aborted.ErrorMessage = &message
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: aborted})
			case <-time.After(time.Millisecond):
				done := &ai.AssistantMessage{
					API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
					Content: ai.ContentList{ai.TextContent{Text: "ack"}}, StopReason: ai.StopStop,
				}
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: done})
			}
		}()
		return stream
	}

	const rounds = 20
	for round := range rounds {
		session := newPromptSession(t, model, streamFn)
		session.Subscribe(func(*SessionEvent) { _ = session.IsStreaming() })
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() { _ = session.Prompt(context.Background(), "hello", nil) })
		}
		wg.Go(func() {
			time.Sleep(time.Duration(round%5) * 200 * time.Microsecond)
			session.Dispose()
		})
		wg.Go(session.AbortAsync)
		wg.Go(session.Dispose)
		settled := make(chan struct{})
		go func() { wg.Wait(); close(settled) }()
		select {
		case <-settled:
		case <-time.After(5 * time.Second):
			t.Fatalf("round %d: a prompt, abort or Dispose did not return", round)
		}

		after := make(chan struct{})
		go func() { _ = session.Prompt(context.Background(), "after", nil); close(after) }()
		select {
		case <-after:
		case <-time.After(5 * time.Second):
			t.Fatalf("round %d: a prompt after Dispose hung", round)
		}
		deadline := time.Now().Add(5 * time.Second)
		for session.IsStreaming() {
			if time.Now().After(deadline) {
				t.Fatalf("round %d: the disposed session is still streaming", round)
			}
			time.Sleep(time.Millisecond)
		}
		session.Dispose()
	}
}
