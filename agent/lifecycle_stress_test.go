package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// Runs started and aborted from several goroutines must each settle: every
// prompt returns, the agent goes idle, every assistant message that starts
// also ends, and no partial message is left behind. Upstream agent.ts (pi
// v1.5.0 f9a26365) serializes these on one event loop; the port's callers can
// overlap.
func TestAgentPromptAndAbortRacesSettle(t *testing.T) {
	streamFn := func(_ *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		partial := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "partial"}}, ai.StopPending)
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: partial})
		go func() {
			select {
			case <-options.Ctx.Done():
				partial.StopReason = ai.StopAborted
				message := ai.RequestAbortedMessage
				partial.ErrorMessage = &message
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: partial})
			case <-time.After(time.Duration(len(options.SessionID)%3) * time.Millisecond):
				done := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "done"}}, ai.StopStop)
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: done})
			}
		}()
		return stream
	}
	a, err := NewAgent(&AgentOptions{StreamFn: streamFn})
	if err != nil {
		t.Fatal(err)
	}
	var starts, ends atomic.Int64
	a.Subscribe(func(event AgentEvent, _ context.Context) error {
		if _, ok := event.Message.(*ai.AssistantMessage); ok {
			switch event.Type {
			case MessageStart:
				starts.Add(1)
			case MessageEnd:
				ends.Add(1)
			}
		}
		_ = a.State() // a listener reading the agent back must not deadlock
		return nil
	})

	const rounds, prompters = 40, 3
	for round := range rounds {
		var wg sync.WaitGroup
		for range prompters {
			// Extra prompts while one runs are rejected as busy; that is fine.
			wg.Go(func() { _ = a.PromptText(context.Background(), "go") })
		}
		if round%2 == 1 {
			wg.Go(func() {
				time.Sleep(time.Duration(round%4) * 100 * time.Microsecond)
				a.Abort()
			})
		}
		wg.Go(func() { _ = a.State() })
		settled := make(chan struct{})
		go func() { wg.Wait(); close(settled) }()
		select {
		case <-settled:
		case <-time.After(5 * time.Second):
			t.Fatalf("round %d: a prompt or abort did not return", round)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.WaitForIdle(ctx); err != nil {
			cancel()
			t.Fatalf("round %d: agent never went idle: %v", round, err)
		}
		cancel()
		state := a.State()
		if state.IsStreaming || state.StreamingMessage != nil || len(state.PendingToolCalls) != 0 {
			t.Fatalf("round %d: idle agent still streaming: %+v", round, state)
		}
		if s, e := starts.Load(), ends.Load(); s != e {
			t.Fatalf("round %d: %d assistant messages started, %d ended", round, s, e)
		}
	}
	if starts.Load() == 0 {
		t.Fatal("no run ever streamed; the stress tested nothing")
	}
}
