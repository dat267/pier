package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// pi v1.4.0 api/openai-completions.ts consumes the OpenAI SDK iterator, which
// throws APIError(undefined, data.error, ...) as soon as a chunk carries a
// truthy `error`, before yielding it.
func TestOpenAIStreamFailsOnStreamedErrorPayload(t *testing.T) {
	events := []string{
		"data: {\"id\":\"c1\",\"model\":\"gpt-5\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"data: {\"error\":{\"message\":\"boom\",\"type\":\"server_error\"}}\n\n",
	}
	server := sseServer(t, events, nil)
	defer server.Close()

	model := testOpenAIModel()
	model.BaseURL = server.URL + "/v1"
	var providerEvents int
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{
		APIKey: "test-key",
		OnProviderStreamEvent: func(data json.RawMessage, eventModel *Model) {
			providerEvents++
		}})

	var types []string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
	}
	if got := strings.Join(types, ","); got != "start,text_start,text_delta,error" {
		t.Fatalf("events = %s", got)
	}
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopError || message.ErrorMessage == nil || *message.ErrorMessage != "boom" {
		t.Fatalf("message = %+v", message)
	}
	if providerEvents != 1 {
		t.Fatalf("provider callbacks = %d; the error chunk must not be yielded", providerEvents)
	}
}
