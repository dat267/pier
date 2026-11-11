package ai

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

// pi v1.5.2 (b4afdddf) api/simple-options.ts clampMaxTokensToContext: the
// safety margin is 15% of the counted input when that exceeds 4096 tokens
// (OpenRouter counted prose up to 12% above the provider's count, #10497), and
// below a 32768-token output room only the fixed margin is kept, with output
// capped at that size. The golden holds the reference's own results.
func TestClampMaxTokensMatchesReference(t *testing.T) {
	raw, err := os.ReadFile("testdata/clamp-max-tokens/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Transcript    string `json:"transcript"`
		ContextWindow int    `json:"contextWindow"`
		MaxTokens     int    `json:"maxTokens"`
		Want          int    `json:"want"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 144 {
		t.Fatalf("corpus has %d cases, want 144", len(cases))
	}
	lengths := map[string]int{"empty": 0, "small": 400, "medium": 40000, "large": 400000}
	for _, test := range cases {
		t.Run(strings.Join([]string{test.Transcript, strconv.Itoa(test.ContextWindow), strconv.Itoa(test.MaxTokens)}, "-"), func(t *testing.T) {
			var messages []Message
			if length := lengths[test.Transcript]; length > 0 {
				messages = []Message{&UserMessage{Content: StringOrBlocks{Text: strings.Repeat("x", length)}, Timestamp: 1}}
			}
			model := &Model{ContextWindow: int64(test.ContextWindow), MaxTokens: 64000}
			got := ClampMaxTokensToContext(model, TranscriptContext{Messages: messages}, test.MaxTokens)
			if got != test.Want {
				t.Fatalf("ClampMaxTokensToContext = %d, want %d", got, test.Want)
			}
		})
	}
}
