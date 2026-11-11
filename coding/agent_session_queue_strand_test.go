package coding

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// A prompt queued as a follow-up or a steer while a run is ending is either
// delivered by that run or starts its own; it is never left queued once the
// session settles. Upstream prompt() (core/agent-session.ts, pi v1.5.2
// b4afdddf) checks isStreaming and queues in one step on its event loop, and
// the agent drains its queues before the run ends, so an accepted message
// always runs.
func TestPromptQueuedAsARunEndsIsNotStranded(t *testing.T) {
	for _, behavior := range []string{"followUp", "steer"} {
		t.Run(behavior, func(t *testing.T) {
			model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
			streamFn := func(m *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				stream := ai.NewAssistantMessageEventStream()
				go func() {
					time.Sleep(time.Duration(rand.IntN(200)) * time.Microsecond)
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
						API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
						Content: ai.ContentList{ai.TextContent{Text: "ok"}},
					}})
				}()
				return stream
			}
			manager, dir := newTestSession(t)
			session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, ConvertToLlm: ConvertToLlm})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Dispose()

			const rounds, queuedPerRound = 60, 3
			for round := range rounds {
				var accepted atomic.Int32
				var group sync.WaitGroup
				group.Go(func() { _ = session.Prompt(context.Background(), fmt.Sprintf("prompt %d", round), nil) })
				for index := range queuedPerRound {
					group.Go(func() {
						// Spread over the run and past its end, so some land in the
						// stretch between the run's last queue check and its end.
						time.Sleep(time.Duration(rand.IntN(2000)) * time.Microsecond)
						text := fmt.Sprintf("queued %d-%d", round, index)
						if session.Prompt(context.Background(), text, &PromptOptions{StreamingBehavior: behavior}) == nil {
							accepted.Add(1)
						}
					})
				}
				group.Wait()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := session.WaitForIdle(ctx)
				cancel()
				if err != nil {
					t.Fatal(err)
				}
				if session.Agent.HasQueuedMessages() {
					t.Fatalf("round %d: a message is still queued after the session settled (steering %v, follow-up %v)",
						round, session.GetSteeringMessages(), session.GetFollowUpMessages())
				}
				delivered := 0
				for _, message := range session.Agent.State().Messages {
					if user, ok := message.(*ai.UserMessage); ok && strings.HasPrefix(contentTextJoinedNoSep(user.Content), fmt.Sprintf("queued %d-", round)) {
						delivered++
					}
				}
				if delivered != int(accepted.Load()) {
					t.Fatalf("round %d: %d of %d accepted prompts reached the transcript", round, delivered, accepted.Load())
				}
			}
		})
	}
}
