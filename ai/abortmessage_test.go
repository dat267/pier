package ai

import (
	"context"
	"testing"
)

// A canceled model request is an abort, not a provider failure. Upstream throws
// Error("Request was aborted") when the request signal is aborted, and the
// transcript renders that as "Operation aborted"; pier surfaced the transport's
// raw "context canceled" in the persisted assistant message instead.
func TestCanceledRequestReportsAbortMessage(t *testing.T) {
	completions := testOpenAIModel()
	completions.BaseURL = "https://127.0.0.1:1/v1" // never reached: the context is already canceled
	responses := testResponsesModel()
	responses.BaseURL = "https://127.0.0.1:1/v1"

	cases := []struct {
		name   string
		stream func(ctx context.Context, model *Model) *AssistantMessageEventStream
		model  *Model
	}{
		{
			name:  "openai-completions",
			model: completions,
			stream: func(ctx context.Context, model *Model) *AssistantMessageEventStream {
				return StreamOpenAICompletionsSimple(model, NormalizeContext(Context{Messages: []Message{
					&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
				}}), &SimpleStreamOptions{StreamOptions: StreamOptions{APIKey: "k", Ctx: ctx}})
			},
		},
		{
			name:  "openai-responses",
			model: responses,
			stream: func(ctx context.Context, model *Model) *AssistantMessageEventStream {
				return StreamOpenAIResponsesSimple(model, NormalizeContext(Context{Messages: []Message{
					&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
				}}), &SimpleStreamOptions{StreamOptions: StreamOptions{APIKey: "k", Ctx: ctx}})
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			message, _ := testCase.stream(ctx, testCase.model).Result(context.Background())
			if message.StopReason != StopAborted {
				t.Fatalf("stopReason = %s, want aborted", message.StopReason)
			}
			if message.ErrorMessage == nil || *message.ErrorMessage != RequestAbortedMessage {
				got := "<nil>"
				if message.ErrorMessage != nil {
					got = *message.ErrorMessage
				}
				t.Fatalf("errorMessage = %q, want %q", got, RequestAbortedMessage)
			}
		})
	}
}
