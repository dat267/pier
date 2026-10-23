package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reference: pi v1.4.0 adc5fa9f8 and 04a93bad8, utils/identity.ts and
// api/*.ts. Exercise the public simple-stream boundary so dropped identity
// options in either the simple adapter or the HTTP adapter are observable.
func TestRequestIdentityHeaders(t *testing.T) {
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	type streamSimple func(*Model, TranscriptContext, *SimpleStreamOptions) *AssistantMessageEventStream
	cases := []struct {
		name   string
		api    string
		stream streamSimple
	}{
		{"anthropic", APIAnthropicMessages, StreamAnthropicSimple},
		{"completions", APIOpenAICompletions, StreamOpenAICompletionsSimple},
		{"responses", APIOpenAIResponses, StreamOpenAIResponsesSimple},
		{"azure", APIAzureOpenAIResponses, StreamAzureOpenAIResponsesSimple},
		{"codex", APIOpenAICodexResponses, StreamOpenAICodexResponsesSimple},
		{"google", APIGoogleGenerativeAI, StreamGoogleGenerativeAISimple},
		{"vertex", APIGoogleVertex, StreamGoogleVertexSimple},
		{"mistral", APIMistralConversations, StreamMistralConversationsSimple},
		{"pi-messages", APIPiMessages, StreamPiMessagesSimple},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"identity", "short", "no attribution", "model override", "request override", "remove"} {
				t.Run(mode, func(t *testing.T) {
					captured := make(chan http.Header, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						captured <- r.Header.Clone()
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"error":{"message":"header probe"}}`))
					}))
					defer server.Close()
					model := &Model{ID: "test-model", API: tc.api, Provider: "openrouter", BaseURL: server.URL,
						ContextWindow: 10000, MaxTokens: 100, Input: []string{"text"}}
					options := &SimpleStreamOptions{StreamOptions: StreamOptions{
						APIKey: "AIzaSyRealKey", Transport: TransportSSE,
						Identity: &ClientIdentity{UserAgent: "acme/1.0", Attribution: PIAttribution},
					}}
					if tc.name == "codex" {
						options.APIKey = codexToken(t, "acct-1")
					}
					wantAgent, wantTitle := "acme/1.0", "pi"
					if mode == "short" {
						options.Identity.UserAgent = "short"
						wantAgent = "pi"
					}
					if mode == "no attribution" {
						options.Identity.Attribution = nil
						wantTitle = ""
					}
					if mode == "model override" || mode == "request override" || mode == "remove" {
						model.Headers = map[string]string{"user-agent": "model-agent", "x-openrouter-title": "model-title"}
						wantAgent, wantTitle = "model-agent", "model-title"
					}
					if mode == "request override" {
						options.Headers = ProviderHeaders{"user-agent": strPtr("request-agent"), "x-openrouter-title": strPtr("request-title")}
						wantAgent, wantTitle = "request-agent", "request-title"
					}
					if mode == "remove" {
						options.Headers = ProviderHeaders{"user-agent": nil, "x-openrouter-title": nil}
						wantAgent, wantTitle = "", ""
					}
					// pi-messages has no model-header layer, only caller headers.
					if tc.name == "pi-messages" && mode == "model override" {
						wantAgent, wantTitle = "acme/1.0", "pi"
					}
					message, err := tc.stream(model, TranscriptContext{Messages: []Message{
						&UserMessage{Content: StringOrBlocks{Text: "hello"}},
					}}, options).Result(context.Background())
					if err != nil || message.StopReason != StopError {
						t.Fatalf("probe result = %+v, %v", message, err)
					}
					select {
					case headers := <-captured:
						if headers.Get("User-Agent") != wantAgent || headers.Get("X-OpenRouter-Title") != wantTitle {
							t.Fatalf("headers = %v; want agent %q, title %q", headers, wantAgent, wantTitle)
						}
					default:
						t.Fatalf("no HTTP request; result = %+v", message)
					}
				})
			}
		})
	}
}
