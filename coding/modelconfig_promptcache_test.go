package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 core/provider-composer.ts carries models.json promptCache into the
// composed model (modelFromJson sets `promptCache: definition.promptCache`) and
// merges an override over the base model's tiers
// (`{ ...model.promptCache, ...override.promptCache }`). Without it, custom
// models lose the cache-warming lifetimes built-in catalogs provide.
func TestModelsJSONPromptCacheReachesModels(t *testing.T) {
	builtin := &ai.Model{
		ID: "builtin", Name: "Builtin", API: ai.APIOpenAIResponses, Provider: "openai",
		BaseURL: "https://openai.test/v1", Type: "chat",
		PromptCache: ai.ModelPromptCache{ai.CacheRetentionShort: 600},
	}
	base := ai.CreateProvider(ai.CreateProviderOptions{
		ID: "openai", Auth: ai.ProviderAuth{APIKey: &ai.ApiKeyAuth{Name: "OpenAI"}}, Models: []*ai.Model{builtin},
	})
	config := &ModelConfig{providers: map[string]ModelsJSONProvider{
		"openai": {
			BaseURL: "https://openai.test/v1",
			API:     ai.APIOpenAIResponses,
			Models: []ModelsJSONModel{{
				ID:          "custom",
				PromptCache: ModelsJSONPromptCache{"short": 120},
			}},
			ModelOverrides: map[string]ModelsJSONModelOverride{
				"builtin": {PromptCache: ModelsJSONPromptCache{"long": 3600}},
			},
		},
	}}
	provider, err := ComposeModelProvider("openai", base, config)
	if err != nil {
		t.Fatal(err)
	}
	models := provider.GetAllModels()
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	overridden, custom := models[0], models[1]
	if overridden.ID != "builtin" || custom.ID != "custom" {
		t.Fatalf("models = %+v", models)
	}
	if got := overridden.PromptCache; got[ai.CacheRetentionShort] != 600 || got[ai.CacheRetentionLong] != 3600 {
		t.Fatalf("override promptCache = %v, want short 600 merged with long 3600", got)
	}
	if got := custom.PromptCache; got[ai.CacheRetentionShort] != 120 {
		t.Fatalf("custom promptCache = %v, want short 120", got)
	}
}
