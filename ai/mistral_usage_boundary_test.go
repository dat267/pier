package ai

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), api/mistral-conversations.ts
// consumeChatStream preserves finite provider usage as JavaScript Numbers.
// D366 saturates counts outside the port's int64 usage representation.
func TestMistralStreamSaturatesFiniteUsageBeyondInt64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: " + `{"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":1e308,"completion_tokens":1e308,"total_tokens":1e308}}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	stream := StreamMistralConversations(mistralModel(server.URL), mistralUserContext(), &MistralOptions{APIKey: "test-key"})
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopStop {
		t.Fatalf("stop reason = %s, error = %v", message.StopReason, message.ErrorMessage)
	}
	if message.Usage.Input != math.MaxInt64 || message.Usage.Output != math.MaxInt64 || message.Usage.TotalTokens != math.MaxInt64 {
		t.Fatalf("usage = %+v, want positive saturated input, output and total", message.Usage)
	}
}
