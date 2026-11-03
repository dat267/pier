package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 f9a26365 api/openai-responses-shared.ts processes
// function_call_arguments.done with ToolCallArgumentsStream.replace, so a
// later output_item.done without arguments finishes the provider's final
// replacement, never a stale streamed prefix.
func TestResponsesKeepsFinalArgumentReplacement(t *testing.T) {
	for _, final := range []string{`{"command":"new"}`, `{"command":`} {
		t.Run(final, func(t *testing.T) {
			encoded, err := MarshalJSON(final)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.Join([]string{
				`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"call","name":"echo","arguments":""}}`,
				`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"command\":\"old\"}"}`,
				fmt.Sprintf(`data: {"type":"response.function_call_arguments.done","output_index":0,"arguments":%s}`, encoded),
				`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"call","name":"echo","arguments":""}}`,
				`data: {"type":"response.completed","response":{"status":"completed","output":[]}}`,
			}, "\n\n") + "\n\n"
			output := &AssistantMessage{StopReason: StopPending}
			if err := ProcessResponsesStream(context.Background(), strings.NewReader(body), output, NewAssistantMessageEventStream(), testResponsesModel(), nil, nil); err != nil {
				t.Fatal(err)
			}
			if len(output.Content) != 1 {
				t.Fatalf("content = %+v", output.Content)
			}
			call := output.Content[0].(ToolCall)
			if final == `{"command":` {
				if string(call.Arguments) != `{}` || !strings.Contains(call.ArgumentsError, "Received arguments:\n"+final) {
					t.Fatalf("malformed replacement lost: %+v", call)
				}
			} else if string(call.Arguments) != final || call.ArgumentsError != "" {
				t.Fatalf("final arguments = %+v, want %s", call, final)
			}
		})
	}
}
