package interactive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// Regression for canceling after provider start but before abort cleanup.
// Upstream v1.1.0 packages/agent/src/agent-loop.ts awaits the terminal message
// and snapshots partial message fields. The UI must receive one balanced
// assistant lifecycle and render one cancellation notice, even if it drains
// the event queue only after provider cleanup has finished.
func TestCancelledProviderRendersOneAbortNotice(t *testing.T) {
	dispatcher, _, transcript, _ := newEventTestDispatcher(t)
	queue := newSessionEventQueue()
	defer queue.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finishProvider := make(chan struct{}, 1)
	providerDone := make(chan struct{})
	partial := &ai.AssistantMessage{Content: ai.ContentList{}, StopReason: ai.StopPending}
	a, err := agent.NewAgent(&agent.AgentOptions{
		StreamFn: func(_ *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
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
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var starts, ends int
	a.Subscribe(func(event agent.AgentEvent, _ context.Context) error {
		if _, ok := event.Message.(*ai.AssistantMessage); ok {
			switch event.Type {
			case agent.MessageStart:
				starts++
				if starts == 1 {
					cancel()
					close(started)
				}
			case agent.MessageEnd:
				ends++
			}
		}
		queue.enqueue(&coding.SessionEvent{Type: event.Type, Agent: &event})
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
	var runFinished bool
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
		runFinished = true
	case <-time.After(20 * time.Millisecond):
	}
	finishProvider <- struct{}{}
	if !runFinished {
		select {
		case err := <-runDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("agent did not settle after provider terminal message")
		}
	}
	<-providerDone
	for len(queue.lossless) > 0 {
		event := <-queue.lossless
		dispatcher.HandleEvent(event.SessionEvent)
	}
	// A terminal-wide invalidation must not reveal an orphaned abort notice.
	transcript.Chat.Invalidate()
	lines := strings.Join(renderChat(t, transcript.Chat), "\n")
	committedAborts := 0
	for _, message := range a.State().Messages {
		if assistant, ok := message.(*ai.AssistantMessage); ok && assistant.StopReason == ai.StopAborted {
			committedAborts++
		}
	}
	if notices := strings.Count(lines, "Operation aborted"); starts != 1 || ends != 1 || committedAborts != 1 || notices != 1 {
		t.Fatalf("assistant starts=%d ends=%d committed aborts=%d displayed notices=%d, want 1 each", starts, ends, committedAborts, notices)
	}
}
