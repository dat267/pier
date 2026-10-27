package ai

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), api/mistral-conversations.ts consumeChatStream
// finalizes toolBlocksByKey.values() in first-seen order, not numeric index or
// Go map order. Nine calls exercise a map spanning more than one bucket.
func TestMistralToolCallsEndInFirstSeenOrder(t *testing.T) {
	order := []int{8, 3, 5, 0, 7, 2, 6, 1, 4}
	var body strings.Builder
	for _, index := range order {
		fmt.Fprintf(&body, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call%d\",\"index\":%d,\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n", index, index)
	}
	body.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	server := sseServer(t, []string{body.String()}, nil)
	defer server.Close()
	stream := StreamMistralConversations(mistralModel(server.URL), mistralUserContext(), &MistralOptions{
		APIKey: "test-key",
	})
	var got []string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		if event.Type == EventError {
			t.Fatalf("stream failed: %+v", event.Error)
		}
		if event.Type == EventToolcallEnd {
			got = append(got, event.ToolCall.ID)
		}
	}
	want := []string{"call8", "call3", "call5", "call0", "call7", "call2", "call6", "call1", "call4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("toolcall_end order = %v, want %v", got, want)
	}
}
