package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), api/openai-completions.ts
// ensureToolCallBlock/getContentIndex retain block identity when an indexed
// delta fills in the id or name after another call has already started.
func TestOpenAICompletionsKeepsInterleavedCallsWithLateIdentity(t *testing.T) {
	for _, first := range []struct {
		name string
		call string
	}{
		{name: "late id", call: `{"index":0,"function":{"name":"read","arguments":"{\"path\":"}}`},
		{name: "late name", call: `{"index":0,"id":"call_a","function":{"arguments":"{\"path\":"}}`},
		{name: "known identity", call: `{"index":0,"id":"call_a","function":{"name":"read","arguments":"{\"path\":"}}`},
	} {
		t.Run(first.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[" + first.call + `,{"index":1,"id":"call_b","function":{"name":"read","arguments":"{\"path\":\"b\"}"}}]}}]}` + "\n\n"))
				_, _ = writer.Write([]byte("data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"read","arguments":"\"a\"}"}}]}}]}` + "\n\n"))
				_, _ = writer.Write([]byte("data: " + `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
			}))
			defer server.Close()
			model := testOpenAIModel()
			model.BaseURL = server.URL
			stream := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{APIKey: "test-key"})
			message, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if message.StopReason != StopToolUse || len(message.Content) != 2 {
				t.Fatalf("message = %+v, want two finished calls", message)
			}
			for i, want := range []struct{ id, args string }{
				{id: "call_a", args: `{"path":"a"}`},
				{id: "call_b", args: `{"path":"b"}`},
			} {
				call, ok := message.Content[i].(ToolCall)
				if !ok || call.ID != want.id || call.Name != "read" || string(call.Arguments) != want.args || call.ArgumentsError != "" {
					t.Errorf("content[%d] = %+v, want %s read(%s)", i, message.Content[i], want.id, want.args)
				}
			}
		})
	}
}
