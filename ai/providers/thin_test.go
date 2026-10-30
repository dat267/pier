package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Tests for the thin provider factories (packages/ai/src/providers/*.ts).

func TestBuiltinProvidersCoverCatalogInOrder(t *testing.T) {
	providers := BuiltinProviders()
	unported := map[string]bool{}
	for _, id := range UnportedBuiltinProviderIDs {
		unported[id] = true
	}

	byID := map[string]*ai.Provider{}
	var order []string
	for _, provider := range providers {
		if byID[provider.ID] != nil {
			t.Fatalf("duplicate provider %s", provider.ID)
		}
		byID[provider.ID] = provider
		order = append(order, provider.ID)
	}
	if len(order) != len(BuiltinProviderIDs)-len(UnportedBuiltinProviderIDs) {
		t.Fatalf("provider count = %d, want %d", len(order), len(BuiltinProviderIDs)-len(UnportedBuiltinProviderIDs))
	}
	// The construction order follows the upstream list.
	expectedIndex := 0
	for _, id := range BuiltinProviderIDs {
		if unported[id] {
			continue
		}
		if order[expectedIndex] != id {
			t.Fatalf("provider %d = %s, want %s", expectedIndex, order[expectedIndex], id)
		}
		expectedIndex++
	}

	// Every catalog provider except the unported adapters is constructed.
	for _, id := range ai.GetBuiltinProviders() {
		if unported[id] {
			if byID[id] != nil {
				t.Fatalf("%s must be skipped until its adapter lands", id)
			}
			continue
		}
		if byID[id] == nil {
			t.Fatalf("catalog provider %s has no built-in factory", id)
		}
	}
}

// pi v1.4.0 at 4a080252 providers/all.ts includes typesafeProvider, whose
// classifier model and transport are defined by typesafe.ts and
// typesafe-system-one.ts.
func TestTypeSafeSystemOneBuiltInProvider(t *testing.T) {
	var provider *ai.Provider
	for _, candidate := range BuiltinProviders() {
		if candidate.ID == "typesafe" {
			provider = candidate
			break
		}
	}
	if provider == nil {
		t.Fatal("typesafe provider is missing from the built-in registry")
	}
	models := provider.GetClassifierModels()
	if len(models) != 1 || models[0].ID != "jev-latest" || models[0].API != "typesafe-system-one" {
		t.Fatalf("classifier models = %+v, want TypeSafe Jev System One model", models)
	}
	if provider.GetClassifierImplementations()["typesafe-system-one"] == nil {
		t.Fatal("typesafe-system-one classifier implementation is missing")
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/systemone" {
			t.Errorf("path = %q, want /v1/systemone", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		var payload struct {
			Model     string                    `json:"model"`
			Questions map[string]map[string]any `json:"questions"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		} else if payload.Model != models[0].ID || payload.Questions["approved"]["type"] != "noul" || payload.Questions["category"]["type"] != "choice" || payload.Questions["satisfaction"]["type"] != "score" {
			t.Errorf("payload = %+v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"answers":{"category":{"type":"choice","choice":"success","probabilities":{"success":0.9,"failure":0.1},"confidence":0.8},"satisfaction":{"type":"score","score":2,"confidence":0.7},"approved":{"type":"noul","noul":0.95}},"usage":{"input_tokens":10,"output_tokens":2}}`))
	}))
	defer server.Close()
	model := *models[0]
	model.BaseURL = server.URL + "/v1/"
	result := provider.Classify(&model, ai.ClassifierContext{
		State: []byte(`{"text":"hello"}`),
		Questions: []ai.ClassifierQuestion{
			{Name: "category", Type: "choice", Instructions: "Classify", Choices: []ai.ClassifierChoice{{Value: "success", Description: "Successful"}, {Value: "failure", Description: "Failed"}}},
			{Name: "satisfaction", Type: "score", Instructions: "Score", Levels: []string{"low", "neutral", "high"}},
			{Name: "approved", Type: "bool", Instructions: "Approve?", BoolCriteria: ai.ClassifierBoolCriteria{True: "yes", False: "no"}},
		},
	}, &ai.ClassifierOptions{StreamOptions: ai.StreamOptions{APIKey: "secret"}})
	if result.StopReason != ai.ClassifierStopStop || result.Answers["approved"].Probability == nil || *result.Answers["approved"].Probability != 0.95 {
		t.Fatalf("classification = %+v", result)
	}
	if result.Answers["category"].Choice != "success" || result.Answers["category"].Probabilities["failure"] != 0.1 || result.Answers["category"].Confidence == nil || *result.Answers["category"].Confidence != 0.8 {
		t.Fatalf("choice answer = %+v", result.Answers["category"])
	}
	if result.Answers["satisfaction"].Score == nil || *result.Answers["satisfaction"].Score != 2 || result.Answers["satisfaction"].Confidence == nil || *result.Answers["satisfaction"].Confidence != 0.7 {
		t.Fatalf("score answer = %+v", result.Answers["satisfaction"])
	}
	if result.Usage == nil || result.Usage.Input != 10 || result.Usage.Output != 2 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

// Reference: pi v1.4.0 (4a080252) packages/ai/src/providers/all.ts and
// providers/{opencode,openrouter,typesafe,vercel-ai-gateway}.ts register these
// providers and their TypeSafe System One classifiers.
func TestSystemOneClassifierProvidersAreRegistered(t *testing.T) {
	byID := map[string]*ai.Provider{}
	for _, provider := range BuiltinProviders() {
		byID[provider.ID] = provider
	}
	for _, id := range []string{"opencode", "openrouter", "typesafe", "vercel-ai-gateway"} {
		provider := byID[id]
		if provider == nil {
			t.Fatalf("%s provider is missing", id)
		}
		if len(provider.GetClassifierModels()) == 0 {
			t.Errorf("%s has no built-in classifier models", id)
		}
		if provider.GetClassifierImplementations()[ai.APITypeSafeSystemOne] == nil {
			t.Errorf("%s has no TypeSafe System One implementation", id)
		}
	}
}

func TestBuiltinProviderSpecs(t *testing.T) {
	byID := map[string]*ai.Provider{}
	for _, provider := range BuiltinProviders() {
		byID[provider.ID] = provider
	}

	expected := map[string]struct{ name, baseURL string }{
		"anthropic":         {"Anthropic", "https://api.anthropic.com"},
		"google":            {"Google", "https://generativelanguage.googleapis.com/v1beta"},
		"openai":            {"OpenAI", "https://api.openai.com/v1"},
		"mistral":           {"Mistral", "https://api.mistral.ai"},
		"deepseek":          {"DeepSeek", "https://api.deepseek.com"},
		"groq":              {"Groq", "https://api.groq.com/openai/v1"},
		"cerebras":          {"Cerebras", "https://api.cerebras.ai/v1"},
		"xai":               {"xAI", "https://api.x.ai/v1"},
		"together":          {"Together", "https://api.together.ai/v1"},
		"baseten":           {"Baseten", "https://inference.baseten.co/v1"},
		"nvidia":            {"NVIDIA", "https://integrate.api.nvidia.com/v1"},
		"moonshotai":        {"Moonshot AI", "https://api.moonshot.ai/v1"},
		"moonshotai-cn":     {"Moonshot AI CN", "https://api.moonshot.cn/v1"},
		"minimax":           {"MiniMax", "https://api.minimax.io/anthropic"},
		"minimax-cn":        {"MiniMax CN", "https://api.minimaxi.com/anthropic"},
		"zai":               {"Z.AI", "https://api.z.ai/api/coding/paas/v4"},
		"zai-coding-cn":     {"Z.AI Coding CN", "https://open.bigmodel.cn/api/coding/paas/v4"},
		"openrouter":        {"OpenRouter", "https://openrouter.ai/api/v1"},
		"vercel-ai-gateway": {"Vercel AI Gateway", "https://ai-gateway.vercel.sh"},
		"kimi-coding":       {"Kimi For Coding", "https://api.kimi.com/coding/v1"},
		"fireworks":         {"Fireworks", "https://api.fireworks.ai/inference"},
		"huggingface":       {"Hugging Face", "https://router.huggingface.co/v1"},
		"github-copilot":    {"GitHub Copilot", "https://api.individual.githubcopilot.com"},
	}
	for id, want := range expected {
		provider := byID[id]
		if provider == nil {
			t.Fatalf("%s missing", id)
		}
		if provider.Name != want.name {
			t.Errorf("%s name = %q, want %q", id, provider.Name, want.name)
		}
		if provider.BaseURL != want.baseURL {
			t.Errorf("%s baseUrl = %q, want %q", id, provider.BaseURL, want.baseURL)
		}
		if len(provider.GetModels()) == 0 {
			t.Errorf("%s has no catalog models", id)
		}
	}
	// Azure and Vertex resolve their base URL per request, and OpenCode's models
	// carry their own base URLs (upstream declares no baseUrl for them).
	for _, id := range []string{"azure", "google-vertex", "opencode", "opencode-go"} {
		if provider := byID[id]; provider == nil || provider.BaseURL != "" {
			t.Fatalf("%s baseUrl = %q, want empty", id, provider.BaseURL)
		}
	}
}

func TestThinSpecsMatchCatalogAPIs(t *testing.T) {
	// Every catalog model's api must be enabled by its spec, otherwise the
	// provider would dispatch to a missing implementation.
	for _, spec := range append(ThinProviderSpecs, EnvAPIKeyProviderSpec{
		ID: "google-vertex", Vertex: true,
	}, EnvAPIKeyProviderSpec{
		ID: "azure", Azure: true, Completions: true,
	}) {
		enabled := map[string]bool{}
		if spec.Anthropic {
			enabled[ai.APIAnthropicMessages] = true
		}
		if spec.Completions {
			enabled[ai.APIOpenAICompletions] = true
		}
		if spec.Responses {
			enabled[ai.APIOpenAIResponses] = true
		}
		if spec.Google {
			enabled[ai.APIGoogleGenerativeAI] = true
		}
		if spec.Mistral {
			enabled[ai.APIMistralConversations] = true
		}
		if spec.Vertex {
			enabled[ai.APIGoogleVertex] = true
		}
		if spec.Azure {
			enabled[ai.APIAzureOpenAIResponses] = true
		}
		for _, model := range ai.GetBuiltinModels(spec.ID) {
			if !enabled[model.API] {
				t.Errorf("%s: model %s uses api %s, which the spec does not enable", spec.ID, model.ID, model.API)
			}
		}
	}
}

func TestProviderDispatchPerAPI(t *testing.T) {
	// A mixed-API provider dispatches per model api; an api without an
	// implementation reports upstream's message.
	provider := EnvAPIKeyProvider(thinByID("opencode"))
	if provider == nil {
		t.Fatal("opencode provider missing")
	}
	models := provider.GetModels()
	if len(models) == 0 {
		t.Fatal("no opencode models")
	}
	seen := map[string]bool{}
	for _, model := range models {
		seen[model.API] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected a mixed-API provider, got %v", seen)
	}
	// A model whose api has no implementation is reported as such.
	unsupported := &ai.Model{ID: "ghost", API: "bedrock-converse-stream", Provider: "opencode"}
	stream := provider.StreamSimple(unsupported, ai.TranscriptContext{}, &ai.SimpleStreamOptions{})
	result, err := stream.Result(context.Background())
	if err != nil {
		t.Fatalf("the stream must resolve with an error message, not fail: %v", err)
	}
	if result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "no API implementation") {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenCodeSessionHeaderWrapping(t *testing.T) {
	var captured *ai.StreamOptions
	recorder := &recordingStreams{onStream: func(options *ai.StreamOptions) { captured = options }}
	wrapped := WithOpenCodeSessionHeader(recorder)

	// A session id adds the routing header.
	wrapped.Stream(&ai.Model{}, ai.TranscriptContext{}, &ai.StreamOptions{SessionID: "session-1"})
	if captured == nil || captured.Headers == nil || captured.Headers[OpenCodeSessionHeader] == nil ||
		*captured.Headers[OpenCodeSessionHeader] != "session-1" {
		t.Fatalf("headers = %#v", captured)
	}
	// An explicit header (any case) is preserved and no duplicate is added.
	custom := "custom"
	wrapped.Stream(&ai.Model{}, ai.TranscriptContext{}, &ai.StreamOptions{
		SessionID: "session-1",
		Headers:   ai.ProviderHeaders{"X-OpenCode-Session": &custom},
	})
	if len(captured.Headers) != 1 || captured.Headers["X-OpenCode-Session"] == nil ||
		*captured.Headers["X-OpenCode-Session"] != "custom" {
		t.Fatalf("headers = %#v", captured.Headers)
	}
	// Without a session id nothing is added.
	wrapped.Stream(&ai.Model{}, ai.TranscriptContext{}, &ai.StreamOptions{})
	if captured.Headers != nil {
		t.Fatalf("headers = %#v", captured.Headers)
	}
	// The simple path forwards too.
	wrapped.StreamSimple(&ai.Model{}, ai.TranscriptContext{}, &ai.SimpleStreamOptions{
		SessionID: "session-2",
	})
	if captured == nil || captured.Headers == nil || *captured.Headers[OpenCodeSessionHeader] != "session-2" {
		t.Fatalf("headers = %#v", captured)
	}
}

type recordingStreams struct {
	onStream func(options *ai.StreamOptions)
}

func (r *recordingStreams) Stream(model *ai.Model, context ai.TranscriptContext, options *ai.StreamOptions) *ai.AssistantMessageEventStream {
	if r.onStream != nil {
		r.onStream(options)
	}
	return ai.NewAssistantMessageEventStream()
}

func (r *recordingStreams) StreamSimple(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	if r.onStream != nil && options != nil {
		r.onStream(&options.StreamOptions)
	}
	return ai.NewAssistantMessageEventStream()
}

func TestGitHubCopilotModelFiltering(t *testing.T) {
	provider := GitHubCopilotProvider()
	models := provider.GetModels()
	if len(models) == 0 {
		t.Fatal("no copilot models")
	}
	// Without a credential every catalog model is offered.
	all := filterModelsForCredential(provider, models, nil)
	if len(all) != len(models) {
		t.Fatalf("filtered = %d, want %d", len(all), len(models))
	}
	// A non-OAuth credential is not filtered either.
	if got := filterModelsForCredential(provider, models, &ai.Credential{Type: ai.CredentialAPIKey}); len(got) != len(models) {
		t.Fatalf("api-key filtered = %d", len(got))
	}
	// An OAuth credential restricts models to the account's available ids.
	rawIDs, _ := json.Marshal([]string{models[0].ID})
	credential := &ai.Credential{Type: ai.CredentialOAuth, OAuth: &ai.OAuthCredential{
		Extra: map[string]json.RawMessage{"availableModelIds": rawIDs},
	}}
	filtered := filterModelsForCredential(provider, models, credential)
	if len(filtered) != 1 || filtered[0].ID != models[0].ID {
		t.Fatalf("filtered = %+v", filtered)
	}
	// A malformed list falls back to every model.
	bad := &ai.Credential{Type: ai.CredentialOAuth, OAuth: &ai.OAuthCredential{
		Extra: map[string]json.RawMessage{"availableModelIds": json.RawMessage(`"nope"`)},
	}}
	if got := filterModelsForCredential(provider, models, bad); len(got) != len(models) {
		t.Fatalf("malformed filtered = %d", len(got))
	}
}

// filterModelsForCredential applies the provider's credential-specific model
// policy.
func filterModelsForCredential(provider *ai.Provider, models []*ai.Model, credential *ai.Credential) []*ai.Model {
	if provider == nil {
		return models
	}
	return ai.ApplyProviderModelFilter(provider, models, credential)
}

func TestBuiltinModelsRegistration(t *testing.T) {
	models := BuiltinModels(ai.CreateModels(nil))
	registered := map[string]bool{}
	for _, provider := range models.GetProviders() {
		registered[provider.ID] = true
	}
	for _, provider := range BuiltinProviders() {
		if !registered[provider.ID] {
			t.Fatalf("%s was not registered", provider.ID)
		}
	}
	if len(registered) != len(BuiltinProviders()) {
		t.Fatalf("registered = %d, providers = %d", len(registered), len(BuiltinProviders()))
	}
	// A catalog model resolves through the registry.
	catalogModels := ai.GetBuiltinModels("deepseek")
	if len(catalogModels) == 0 {
		t.Fatal("no deepseek catalog models")
	}
	want := catalogModels[0]
	if model := models.GetModel("deepseek", want.ID); model == nil || model.Provider != "deepseek" {
		t.Fatalf("model = %+v, want %s", model, want.ID)
	}
}
