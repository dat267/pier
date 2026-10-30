package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestOpenAIProviderFiltersClassifierForChatGPTOAuth follows openai.ts, which
// hides classifier models for ChatGPT OAuth credentials.
func TestOpenAIProviderFiltersClassifierForChatGPTOAuth(t *testing.T) {
	provider := OpenAIProvider()
	models := append(provider.GetModels(), provider.GetClassifierModels()...)
	oauthModels := provider.FilterAllModels(models, &ai.Credential{Type: ai.CredentialOAuth})
	for _, model := range oauthModels {
		if model.Type == "classifier" {
			t.Fatalf("classifier model %s remained available to ChatGPT OAuth", model.ID)
		}
	}
	apiKeyModels := provider.FilterAllModels(models, &ai.Credential{Type: ai.CredentialAPIKey})
	if len(apiKeyModels) != len(models) {
		t.Fatalf("API-key model count = %d, want %d", len(apiKeyModels), len(models))
	}
}

// TestOpenAIProviderExposesDecisionsClassifier follows openai.ts and
// openai.models.ts, which register and export the Decisions classifier model.
func TestOpenAIProviderExposesDecisionsClassifier(t *testing.T) {
	models := OpenAIProvider().GetClassifierModels()
	if len(models) != 1 {
		t.Fatalf("classifier models = %d, want one OpenAI Decisions model", len(models))
	}
	model := models[0]
	if model.ID != "gpt-6-luna" || model.API != ai.APIOpenAIDecisions || model.Type != "classifier" {
		t.Fatalf("classifier model = %+v", model)
	}
}

// Reference: pi v1.4.0 packages/ai/src/api/openai-decisions.ts caps input at 128 images.
func TestOpenAIProviderClassifiesRejectsMoreThan128Images(t *testing.T) {
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &ai.Model{ID: "gpt-6-luna", API: ai.APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text", "image"}}
	images := make([]ai.ImageContent, 129)
	result := OpenAIProvider().Classify(model, ai.ClassifierContext{State: json.RawMessage(`{}`), Images: images}, &ai.ClassifierOptions{StreamOptions: ai.StreamOptions{APIKey: "secret"}})
	if len(requests) != 0 {
		t.Fatalf("classifier made %d requests for oversized image input", len(requests))
	}
	if result.StopReason != ai.ClassifierStopError || result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "at most 128 images, got 129") {
		t.Fatalf("oversized image result = %+v", result)
	}
}

// Reference: pi v1.4.0 packages/ai/src/api/openai-decisions.ts and
// packages/ai/test/openai-decisions.test.ts.
func TestOpenAIProviderClassifiesWithDecisionsAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/decisions" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("authorization = %q", got)
		}
		var request struct {
			Model     string `json:"model"`
			Input     string `json:"input"`
			Questions []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Model != "gpt-6-luna" || request.Input != `{"text":"hi"}` || len(request.Questions) != 1 || request.Questions[0] != (struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}{Type: "predicate", Name: "approved"}) {
			t.Errorf("request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":[{"type":"predicate","name":"approved","probability":0.9}]}`))
	}))
	defer server.Close()

	model := &ai.Model{
		ID: "gpt-6-luna", Name: "GPT-6 Luna", API: ai.APIOpenAIDecisions,
		Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier",
		Input: []string{"text", "image"}, Cost: ai.ModelCost{ModelCostRates: ai.ModelCostRates{Input: 0.1}},
		ContextWindow: 922000,
	}
	result := OpenAIProvider().Classify(model, ai.ClassifierContext{
		State: json.RawMessage(`{"text":"hi"}`),
		Questions: []ai.ClassifierQuestion{{
			Name: "approved", Type: "bool", Instructions: "Is this approved?",
			BoolCriteria: ai.ClassifierBoolCriteria{True: "yes", False: "no"},
		}},
	}, &ai.ClassifierOptions{StreamOptions: ai.StreamOptions{APIKey: "secret"}})
	if result.StopReason != ai.ClassifierStopStop {
		t.Fatalf("stop reason = %q; error = %v", result.StopReason, result.ErrorMessage)
	}
	answer, ok := result.Answers["approved"]
	if !ok || answer.Type != "bool" || answer.Probability == nil || *answer.Probability != 0.9 {
		t.Fatalf("answer = %+v", answer)
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
