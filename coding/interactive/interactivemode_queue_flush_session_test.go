package interactive

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// Upstream flushCompactionQueue (interactive-mode.ts, pi v1.5.2 b4afdddf)
// starts the first queued prompt without awaiting it and queues the rest into
// that run, so every message queued during the compaction runs. The rest must
// not wait for the first prompt's run to end: queued into the idle session
// afterwards, they would sit in the queue with nothing to take them.
func TestCompactionQueueFlushDeliversEveryQueuedMessage(t *testing.T) {
	controller, _, _ := newQueueTestController(t)
	dir := t.TempDir()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{SessionDir: dir})
	session, err := coding.NewAgentSession(&coding.SessionConfig{
		Cwd: dir, Sessions: manager, ConvertToLlm: coding.ConvertToLlm,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128},
		StreamFn: func(m *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				time.Sleep(time.Millisecond)
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
					API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "ok"}},
				}})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	controller.Session = session

	controller.QueueCompactionMessage("first", "followUp")
	controller.QueueCompactionMessage("second", "followUp")
	controller.QueueCompactionMessage("third", "steer")
	controller.FlushCompactionQueue(context.Background(), false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	delivered := map[string]bool{}
	for _, message := range session.Agent.State().Messages {
		if user, ok := message.(*ai.UserMessage); ok {
			delivered[ai.ContentText(user.Content, "")] = true
		}
	}
	for _, text := range []string{"first", "second", "third"} {
		if !delivered[text] {
			t.Errorf("%q never ran (steering %v, follow-up %v)", text, session.GetSteeringMessages(), session.GetFollowUpMessages())
		}
	}
}
