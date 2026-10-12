package agent

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// Upstream v1.1.0 packages/agent/src/agent-loop.ts drains the provider iterator
// and awaits response.result() without using the abort signal to cancel those
// waits. The signal cancels provider work, whose terminal message closes the
// existing assistant lifecycle instead of creating another failure message.
func TestAgentCancellationWaitsForProviderTerminal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finishProvider := make(chan struct{}, 1)
	providerDone := make(chan struct{})
	partial := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "partial response"}}, ai.StopPending)
	streamFn := func(_ *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: partial})
		go func() {
			defer close(providerDone)
			<-options.Ctx.Done()
			<-finishProvider
			partial.StopReason = ai.StopAborted
			message := ai.RequestAbortedMessage
			partial.ErrorMessage = &message
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: partial})
			stream.End(&partial)
		}()
		return stream
	}
	a, err := NewAgent(&AgentOptions{StreamFn: streamFn})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var starts, ends int
	var final *ai.AssistantMessage
	a.Subscribe(func(event AgentEvent, _ context.Context) error {
		if message, ok := event.Message.(*ai.AssistantMessage); ok {
			switch event.Type {
			case MessageStart:
				starts++
				if starts == 1 {
					cancel()
					close(started)
				}
			case MessageEnd:
				ends++
				final = message
			}
		}
		return nil
	})
	runDone := make(chan error, 1)
	go func() { runDone <- a.PromptText(ctx, "test") }()
	defer func() {
		cancel()
		close(finishProvider)
		select {
		case <-providerDone:
		case <-time.After(time.Second):
			t.Error("provider cleanup did not finish")
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("assistant did not start")
	}
	select {
	case err := <-runDone:
		t.Fatalf("agent settled before provider abort cleanup completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	// Let cleanup finish without closing the channel a second time in defer.
	finishProvider <- struct{}{}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("agent did not settle after provider terminal message")
	}
	if starts != 1 || ends != 1 {
		t.Fatalf("assistant lifecycle: starts=%d ends=%d, want 1 each", starts, ends)
	}
	if final != partial || final.StopReason != ai.StopAborted || final.Content[0].(ai.TextContent).Text != "partial response" {
		t.Fatalf("provider's aborted partial was not preserved: %+v", final)
	}
}

// Upstream v1.1.0 agent-loop.ts emits {...partialMessage} for message_start
// and message_update. Later provider cleanup must not change the message
// fields already handed to listeners or queued for the UI.
func TestAgentPartialEventsKeepTheirMessageFields(t *testing.T) {
	partial := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "first"}}, ai.StopPending)
	stream := ai.NewAssistantMessageEventStream()
	a, err := NewAgent(&AgentOptions{
		StreamFn: func(_ *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: partial})
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var started, updated *ai.AssistantMessage
	a.Subscribe(func(event AgentEvent, _ context.Context) error {
		message, ok := event.Message.(*ai.AssistantMessage)
		if !ok {
			return nil
		}
		switch event.Type {
		case MessageStart:
			started = message
			partial.Content = ai.ContentList{ai.TextContent{Text: "updated"}}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Partial: partial, Delta: "updated"})
		case MessageUpdate:
			updated = message
			partial.StopReason = ai.StopAborted
			abortMessage := ai.RequestAbortedMessage
			partial.ErrorMessage = &abortMessage
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: partial})
			stream.End(&partial)
		}
		return nil
	})
	if err := a.PromptText(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	for name, message := range map[string]*ai.AssistantMessage{"start": started, "update": updated} {
		if message == nil || message.StopReason != ai.StopPending || message.ErrorMessage != nil {
			t.Fatalf("%s message changed after provider cleanup: %+v", name, message)
		}
	}
	if text := started.Content[0].(ai.TextContent).Text; text != "first" {
		t.Fatalf("start content = %q, want first", text)
	}
	if text := updated.Content[0].(ai.TextContent).Text; text != "updated" {
		t.Fatalf("update content = %q, want updated", text)
	}
}
