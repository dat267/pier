package ai

import (
	"context"
	"testing"
)

// Every simple stream shares one setup contract: nil options default to an
// empty set, and a missing API key ends the stream with a single error message
// naming the model's provider. Pinned by pi v1.4.0 streamSimple in each api
// module.
func TestSimpleStreamsReportMissingAPIKey(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		api      string
		stream   func(*Model, TranscriptContext, *SimpleStreamOptions) *AssistantMessageEventStream
	}{
		{"azure", "azure", APIAzureOpenAIResponses, StreamAzureOpenAIResponsesSimple},
		{"codex", "openai-codex", APIOpenAICodexResponses, StreamOpenAICodexResponsesSimple},
		{"google", "google", APIGoogleGenerativeAI, StreamGoogleGenerativeAISimple},
		{"mistral", "mistral", APIMistralConversations, StreamMistralConversationsSimple},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := &Model{ID: "m", API: test.api, Provider: test.provider}
			message, err := test.stream(model, NormalizeContext(Context{}), nil).Result(context.Background())
			if err != nil {
				t.Fatalf("Result: %v", err)
			}
			if message == nil || message.StopReason != StopError || message.ErrorMessage == nil {
				t.Fatalf("message = %+v, want a stop-error message", message)
			}
			if want := "No API key for provider: " + test.provider; *message.ErrorMessage != want {
				t.Fatalf("error message = %q, want %q", *message.ErrorMessage, want)
			}
			if message.API != test.api || message.Provider != test.provider || message.Model != model.ID {
				t.Fatalf("message identity = %s/%s/%s", message.API, message.Provider, message.Model)
			}
		})
	}
}
