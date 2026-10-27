package ai

import (
	"context"
	"testing"
)

// pi v1.4.0 api/google-generative-ai.ts keys block creation on
// `part.text !== undefined`, so an empty text part still opens a block and
// emits a delta.
func TestGoogleStreamKeepsPresentButEmptyTextParts(t *testing.T) {
	events := []string{
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"\"}]}}]}\n\n",
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"id\":\"call_1\",\"name\":\"lookup\",\"args\":{}}}]},\"finishReason\":\"STOP\"}]}\n\n",
	}
	server := sseServer(t, events, nil)
	defer server.Close()

	model := testGoogleModel()
	model.BaseURL = server.URL + "/v1beta"
	stream := StreamGoogleGenerativeAI(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &GoogleOptions{APIKey: "test-key"})

	var types []string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
	}
	if got, want := joinTypes(types), "start,text_start,text_delta,text_end,toolcall_start,toolcall_delta,toolcall_end,done"; got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 2 {
		t.Fatalf("content = %+v", message.Content)
	}
	if text, ok := message.Content[0].(TextContent); !ok || text.Text != "" {
		t.Fatalf("first block = %+v", message.Content[0])
	}
}

func joinTypes(types []string) string {
	out := ""
	for i, value := range types {
		if i > 0 {
			out += ","
		}
		out += value
	}
	return out
}
