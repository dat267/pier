package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// TestOpenAIProviderUsesResponses follows packages/ai/src/providers/openai.ts:
// the built-in OpenAI factory uses openAIResponsesApi, not Chat Completions.
// ChatGPT OAuth rejects /chat/completions with no_matching_rule even though
// the same credential is authorized for /responses.
func TestOpenAIProviderUsesResponses(t *testing.T) {
	for _, simple := range []bool{false, true} {
		t.Run(map[bool]string{false: "Stream", true: "StreamSimple"}[simple], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/responses" {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":{"message":"Unauthorized","type":"rejected_by_access_enforcement","code":"no_matching_rule"},"status":401}`))
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\"}}\n\n"))
			}))
			defer server.Close()
			provider := OpenAIProvider()
			model := *ai.GetBuiltinModel("openai", "gpt-6.1-sol")
			model.BaseURL = server.URL + "/v1"
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			options := ai.StreamOptions{APIKey: "oauth-access-token", Ctx: ctx}
			var stream *ai.AssistantMessageEventStream
			if simple {
				stream = provider.StreamSimple(&model, ai.TranscriptContext{}, &ai.SimpleStreamOptions{StreamOptions: options})
			} else {
				stream = provider.Stream(&model, ai.TranscriptContext{}, &options)
			}
			message, err := stream.Result(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if message.ErrorMessage != nil {
				t.Fatalf("OpenAI request failed: %s", *message.ErrorMessage)
			}
			if message.StopReason != ai.StopStop {
				t.Fatalf("stop reason = %q, want stop", message.StopReason)
			}
		})
	}
}

// TestOpenAIProviderChatGPTOAuth pins that the OpenAI provider advertises the
// ChatGPT-subscription OAuth alongside its api key (upstream openaiProvider).
func TestOpenAIProviderChatGPTOAuth(t *testing.T) {
	provider := OpenAIProvider()
	if provider.Auth.APIKey == nil {
		t.Fatal("openai has no api-key auth")
	}
	if provider.Auth.OAuth == nil {
		t.Fatal("openai has no oauth auth")
	}
	if provider.Auth.OAuth.Name != "OpenAI (ChatGPT subscription)" {
		t.Fatalf("oauth name = %q", provider.Auth.OAuth.Name)
	}
	if provider.Auth.OAuth.LoginLabel != "Sign in with ChatGPT" || !provider.Auth.OAuth.IsSubscription {
		t.Fatalf("oauth = %+v", provider.Auth.OAuth)
	}
}
