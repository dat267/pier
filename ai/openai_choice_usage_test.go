package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), api/openai-completions.ts falls
// back to choice.usage when !chunk.usage, including an explicit null.
func TestOpenAICompletionsUsesChoiceUsageWhenTopLevelIsNull(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
		input int64
	}{
		{name: "null", field: `"usage":null,`, input: 10},
		{name: "missing", input: 10},
		{name: "top level wins", field: `"usage":{"prompt_tokens":20,"completion_tokens":4},`, input: 20},
		{name: "empty top level wins", field: `"usage":{},`, input: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte("data: {" + test.field + `"choices":[{"finish_reason":"stop","delta":{},"usage":{"prompt_tokens":10,"completion_tokens":4}}]}` + "\n\ndata: [DONE]\n\n"))
			}))
			defer server.Close()
			model := testOpenAIModel()
			model.BaseURL = server.URL
			stream := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{APIKey: "test-key"})
			message, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if message.StopReason != StopStop {
				t.Fatalf("stop reason = %s, error = %v", message.StopReason, message.ErrorMessage)
			}
			if message.Usage.Input != test.input {
				t.Fatalf("input usage = %d, want %d", message.Usage.Input, test.input)
			}
		})
	}
}
