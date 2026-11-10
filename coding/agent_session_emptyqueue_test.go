package coding

import (
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// An image-only steer carries no text, so its display entry is "". The
// reference guards the dequeue with `if (messageText)` (agent-session.ts:1141,
// pi v1.5.0 f9a26365), so an empty entry is never removed and the pending
// count never returns to zero. pier removes it.
//
// D437: remove the empty entry, so the dock banner clears when the message is
// pushed into the transcript.
func TestQueueDisplayRemovalHandlesEmptyText(t *testing.T) {
	session := &AgentSession{steeringMessages: []string{""}}
	session.handleAgentEvent(&agent.AgentEvent{
		Type: agent.MessageStart,
		Message: &ai.UserMessage{
			Content: ai.StringOrBlocks{Blocks: ai.ContentList{
				ai.ImageContent{Data: "abc", MimeType: "image/png"},
			}},
		},
	})
	if len(session.steeringMessages) != 0 {
		t.Fatalf("steering display queue = %q, want empty", session.steeringMessages)
	}
}
