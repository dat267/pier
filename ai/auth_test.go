package ai

import (
	"context"
	"encoding/json"
	"testing"
)

// auth/context.ts defaultProviderAuthContext uses JavaScript String.trim.
func TestDefaultAuthContextTrimsJavaScriptWhitespace(t *testing.T) {
	t.Setenv("PIER_TEST_AUTH_BOM", "\uFEFF")
	if value, ok := (DefaultAuthContext{}).Env("PIER_TEST_AUTH_BOM"); ok || value != "" {
		t.Fatalf("Env() = %q, %v; want whitespace-only value unset", value, ok)
	}
}

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
				}}), &SimpleStreamOptions{APIKey: "k", Ctx: ctx})
			},
		},
		{
			name:  "openai-responses",
			model: responses,
			stream: func(ctx context.Context, model *Model) *AssistantMessageEventStream {
				return StreamOpenAIResponsesSimple(model, NormalizeContext(Context{Messages: []Message{
					&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
				}}), &SimpleStreamOptions{APIKey: "k", Ctx: ctx})
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

// Reference: pi v1.6.0 (faac5858) core/auth-storage.ts writes
// `JSON.stringify(data, null, 2)`, so a member that is present is kept even when
// it is empty: `{ type: "api_key", key: "" }` stays exactly that, and so does
// `env: {}`. The port's struct tags were `omitempty`, which dropped both, so a
// login answered with an empty secret persisted without the member the
// reference keeps.
func TestCredentialKeepsPresentEmptyMembers(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"type":"api_key","key":""}`, `{"type":"api_key","key":""}`},
		{`{"type":"api_key","key":"","env":{}}`, `{"type":"api_key","key":"","env":{}}`},
		{`{"type":"api_key","key":"k","env":{"A":"1"}}`, `{"type":"api_key","key":"k","env":{"A":"1"}}`},
		// An absent member stays absent: JSON.stringify drops undefined.
		{`{"type":"api_key"}`, `{"type":"api_key"}`},
		{`{"type":"api_key","key":"k"}`, `{"type":"api_key","key":"k"}`},
	} {
		var credential Credential
		if err := json.Unmarshal([]byte(tc.raw), &credential); err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		got, err := MarshalJSON(&credential)
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s round-trip = %s, want %s (JSON.stringify keeps a present member)", tc.raw, got, tc.want)
		}
	}
}
