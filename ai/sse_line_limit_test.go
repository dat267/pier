package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// pi v1.4.0's OpenAI adapter delegates to the SDK LineDecoder, which has no
// fixed event-line limit (openai/src/internal/decoders/line.ts).
func TestOpenAIChunkParserAcceptsLargeDataLine(t *testing.T) {
	payload := `{"choices":[{"delta":{"content":"` + strings.Repeat("x", 16*1024*1024) + `"}}]}`
	body := "data: " + payload + "\n\n"
	var emitted json.RawMessage
	err := iterateOpenAIChunks(context.Background(), strings.NewReader(body), func(raw json.RawMessage) {
		emitted = raw
	})
	if err != nil {
		t.Fatalf("iterateOpenAIChunks returned error for a valid large event: %v", err)
	}
	if len(emitted) != len(payload) {
		t.Fatalf("emitted payload length = %d, want %d", len(emitted), len(payload))
	}
}
