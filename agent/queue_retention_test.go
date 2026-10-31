package agent

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"weak"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), agent.ts PendingMessageQueue.drain replaces the
// queue with messages.slice(drained.length), releasing consumed slots. Once
// the transcript is replaced too, the delivered message must be collectible.
func TestDrainedAgentQueuesReleaseDeliveredMessages(t *testing.T) {
	for _, followUp := range []bool{false, true} {
		for _, mode := range []QueueMode{QueueModeOneAtATime, QueueModeAll} {
			t.Run(fmt.Sprintf("followUp=%t/mode=%s", followUp, mode), func(t *testing.T) {
				a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn(), SteeringMode: mode, FollowUpMode: mode})
				if err != nil {
					t.Fatal(err)
				}
				message := enqueueAgentRetentionMessage(a, followUp)
				if err := a.PromptText(context.Background(), "start"); err != nil {
					t.Fatal(err)
				}
				if a.HasQueuedMessages() {
					t.Fatal("run did not consume the queued message")
				}
				// Keeping it in the active transcript is legitimate. Replacing
				// that transcript must not leave an empty queue owning it.
				a.SetMessages(nil)
				runtime.GC()
				if message.Value() != nil {
					t.Fatal("empty queue retains a delivered message after transcript replacement")
				}
				runtime.KeepAlive(a)
			})
		}
	}
}

func enqueueAgentRetentionMessage(a *Agent, followUp bool) weak.Pointer[ai.UserMessage] {
	message := createUserMessage(strings.Repeat("queued", 1024))
	if followUp {
		a.FollowUp(message)
	} else {
		a.Steer(message)
	}
	return weak.Make(message)
}
