package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), api/openai-responses-shared.ts uses
// item.input ?? getCustomToolCallInput(block), not a truthiness fallback.
// Expectations were replayed through the actual upstream stream: absent/null
// input retains accumulated input, but an explicit empty string must not rewind it.
func TestResponsesCustomToolFinalInputDoesNotRewind(t *testing.T) {
	for _, c := range []struct {
		name, finalInput string
		want             StopReason
	}{
		{"missing", "", StopToolUse},
		{"null", `,"input":null`, StopToolUse},
		{"empty", `,"input":""`, StopError},
		{"complete", `,"input":"echo harmless"`, StopToolUse},
	} {
		t.Run(c.name, func(t *testing.T) {
			server := sseServer(t, []string{
				"data: " + `{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"echo","input":""}}` + "\n\n",
				"data: " + `{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"echo harmless"}` + "\n\n",
				fmt.Sprintf("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"custom_tool_call\",\"id\":\"ctc_1\",\"call_id\":\"call_1\",\"name\":\"echo\"%s}}\n\n", c.finalInput),
				"data: " + `{"type":"response.completed","response":{"status":"completed","output":[]}}` + "\n\n",
			}, nil)
			defer server.Close()
			model := testResponsesModel()
			model.BaseURL = server.URL
			result, err := StreamOpenAIResponses(model, TranscriptContext{Messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
			}}, &OpenAIResponsesOptions{StreamOptions: StreamOptions{APIKey: "test-key"}}).Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.StopReason != c.want {
				t.Fatalf("stop = %s, want %s; error = %v", result.StopReason, c.want, result.ErrorMessage)
			}
			if c.want == StopError {
				if result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "changed non-monotonically") {
					t.Fatalf("error = %v", result.ErrorMessage)
				}
			} else if call, ok := result.Content[0].(ToolCall); !ok || string(call.Arguments) != `{"input":"echo harmless"}` {
				t.Fatalf("content = %+v", result.Content)
			}
		})
	}
}
