package coding

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// customContentToBlocks normalizes string|blocks content. The reference keeps
// string content as one text block and passes anything else through
// (core/messages.ts:163, `typeof m.content === "string"`), so a null content
// adds no block. Decoding into a Go string first turned null into "" and
// produced one empty text block (D430).
func TestCustomContentNullAddsNoBlock(t *testing.T) {
	null := customContentToBlocks(json.RawMessage(`null`))
	if len(null.Blocks) != 0 {
		t.Fatalf("null content produced %d blocks: %v", len(null.Blocks), null.Blocks)
	}
	if text := customContentToBlocks(json.RawMessage(`""`)); len(text.Blocks) != 1 {
		t.Fatalf("an empty string is still string content: %d blocks", len(text.Blocks))
	}
	plain := customContentToBlocks(json.RawMessage(`"hello"`))
	if len(plain.Blocks) != 1 {
		t.Fatalf("string content produced %d blocks", len(plain.Blocks))
	}
	if block, ok := plain.Blocks[0].(ai.TextContent); !ok || block.Text != "hello" {
		t.Fatalf("string content = %v", plain.Blocks)
	}
	blocks := customContentToBlocks(json.RawMessage(`[{"type":"text","text":"x"}]`))
	if len(blocks.Blocks) != 1 {
		t.Fatalf("block content produced %d blocks", len(blocks.Blocks))
	}
}

// The request must carry no stray empty block for a null custom content.
func TestCustomMessageNullContentReachesNoBlock(t *testing.T) {
	raw := json.RawMessage(`{"customType":"note","content":null}`)
	out := ConvertToLlm([]ai.Message{&ai.CustomMessage{Role: "custom", Content: raw}})
	if len(out) != 1 {
		t.Fatalf("custom message produced %d messages", len(out))
	}
	user, ok := out[0].(*ai.UserMessage)
	if !ok {
		t.Fatalf("custom message converted to %T", out[0])
	}
	if len(user.Content.Blocks) != 0 {
		t.Fatalf("null content reached the request as %v", user.Content.Blocks)
	}
}
