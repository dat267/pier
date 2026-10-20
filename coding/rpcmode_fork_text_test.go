package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.3.0 core/agent-session-runtime.ts extractUserMessageText
// joins text blocks without a separator for a "before" fork.
func TestRPCUserMessageEntryTextJoinsBlockContent(t *testing.T) {
	raw, err := ai.MarshalMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Blocks: ai.ContentList{
		ai.TextContent{Text: "first "}, ai.TextContent{Text: "question"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	text, ok := rpcUserMessageEntryText(&SessionEntry{Type: "message", Message: raw})
	if !ok || text != "first question" {
		t.Errorf("text = %q, ok = %v; want the joined block text", text, ok)
	}
}
