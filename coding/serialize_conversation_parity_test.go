package coding

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/compaction/utils.ts serializeConversation.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/serialize-conversation/generate.mjs ./pi > coding/testdata/serialize-conversation/golden.json
func TestSerializeConversationMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/serialize-conversation/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string            `json:"name"`
		Messages []json.RawMessage `json:"messages"`
		Expected string            `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty serialize-conversation corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			messages := make([]ai.Message, 0, len(fixture.Messages))
			for _, encoded := range fixture.Messages {
				message, err := ai.UnmarshalMessage(encoded)
				if err != nil {
					t.Fatalf("decode message: %v", err)
				}
				messages = append(messages, message)
			}
			if got := SerializeConversation(messages); got != fixture.Expected {
				t.Fatalf("serialized conversation differs\n got: %q\nwant: %q", got, fixture.Expected)
			}
		})
	}
}
