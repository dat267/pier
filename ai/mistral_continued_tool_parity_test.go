package ai

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), api/mistral-conversations.ts consumeChatStream:
// every tool delta ends the active text/thinking block and clears currentBlock,
// even when it updates an earlier tool call rather than appending a new one.
func TestMistralContinuedToolCallClosesTextBlock(t *testing.T) {
	chunks := []string{
		`{"choices":[{"delta":{"tool_calls":[{"id":"abc123XYZ","index":0,"function":{"name":"lookup","arguments":"{\"pa"}}]}}]}`,
		`{"choices":[{"delta":{"content":"middle"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":"th\":\"a\"}"}}]}}]}`,
		`{"choices":[{"delta":{"content":"after"},"finish_reason":"tool_calls"}]}`,
	}
	var sse strings.Builder
	for _, chunk := range chunks {
		sse.WriteString("data: " + chunk + "\n\n")
	}
	sse.WriteString("data: [DONE]\n\n")
	server := sseServer(t, []string{sse.String()}, nil)
	defer server.Close()
	stream := StreamMistralConversations(mistralModel(server.URL), mistralUserContext(), &MistralOptions{
		APIKey: "test-key",
	})
	var types []string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
	}
	want := []string{"start", "toolcall_start", "toolcall_delta", "text_start", "text_delta", "text_end", "toolcall_delta", "text_start", "text_delta", "text_end", "toolcall_end", "done"}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("events = %v, want %v", types, want)
	}
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 3 {
		t.Fatalf("content = %+v, want tool call and two separate text blocks", message.Content)
	}
	for i, want := range []string{"middle", "after"} {
		text, ok := message.Content[i+1].(TextContent)
		if !ok || text.Text != want {
			t.Errorf("content[%d] = %+v, want text %q", i+1, message.Content[i+1], want)
		}
	}
}
