package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// credentialPrintProvider builds a stub provider with the given model ids.
func credentialPrintProvider(id string, modelIDs ...string) *ai.Provider {
	models := make([]*ai.Model, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, &ai.Model{
			ID: modelID, Name: modelID, API: ai.APIOpenAICompletions, Provider: id,
			BaseURL: "https://" + id + ".example.com", Input: []string{"text"},
			ContextWindow: 1000, MaxTokens: 100,
		})
	}
	return ai.CreateProvider(ai.CreateProviderOptions{
		ID: id, Name: id + " provider", BaseURL: "https://" + id + ".example.com",
		Auth: ai.ProviderAuth{APIKey: &ai.ApiKeyAuth{
			Name: id + " key",
			Resolve: func(input ai.AuthResolveInput) (*ai.AuthResult, error) {
				if input.Credential == nil || input.Credential.Key == "" {
					return nil, nil
				}
				return &ai.AuthResult{
					Auth:   ai.ModelAuth{APIKey: input.Credential.Key},
					Env:    ai.ProviderEnv(input.Credential.Env),
					Source: "stored credential",
				}, nil
			},
		}},
		Models: models,
		Single: funcStreams{},
	})
}

// Upstream v1.4.0 #10913 (credential-print.test.ts): with only --model, a
// credential is printed only for providers whose catalog holds that model, so
// an uncatalogued id on another configured provider does not count as a match.
func TestResolveCredentialForPrintResolvesOnlyCatalogModels(t *testing.T) {
	store := newMemoryCredentialStore()
	store.entries["openai"] = &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "openai-test-key"}}
	store.entries["anthropic"] = &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "anthropic-test-key"}}
	runtime := runtimeWithCredentialPrintProviders(t, store,
		credentialPrintProvider("openai", "gpt-5.5"),
		credentialPrintProvider("anthropic", "claude-opus-5"),
	)

	model := "gpt-5.5"
	got, err := ResolveCredentialForPrint(&Args{Model: &model}, runtime, AuthCommandPrintAPIKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "openai-test-key" {
		t.Fatalf("credential = %q; want the openai key", got)
	}
}

func runtimeWithCredentialPrintProviders(t *testing.T, store *memoryCredentialStore, providers ...*ai.Provider) *ModelRuntime {
	t.Helper()
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		DisableModelsJSON: true,
		RefreshOnCreate:   boolPtr(false),
		Credentials:       store,
		ModelsStore:       NewInMemoryCodingAgentModelsStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.defaults = map[string]*ai.Provider{}
	runtime.builtins = map[string]*ai.Provider{}
	for _, provider := range providers {
		runtime.defaults[provider.ID] = provider
		runtime.builtins[provider.ID] = provider
	}
	runtime.rebuildProviders()
	return runtime
}
