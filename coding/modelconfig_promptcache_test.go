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
func TestModelsJSONSaturatesLargePromptCacheAndCostNumbers(t *testing.T) {
	base := ai.CreateProvider(ai.CreateProviderOptions{ID: "custom", Models: []*ai.Model{}})
	config := &ModelConfig{providers: map[string]ModelsJSONProvider{
		"custom": {
			BaseURL: "https://provider.test/v1", API: ai.APIOpenAIResponses,
			Models: []ModelsJSONModel{{
				ID: "large", PromptCache: ModelsJSONPromptCache{"short": 1e308},
				Cost: &ModelsJSONCost{ModelsJSONCostRates: ModelsJSONCostRates{Input: 1, Output: 1},
					Tiers: []ModelsJSONCostTier{{InputTokensAbove: 1e308, ModelsJSONCostRates: ModelsJSONCostRates{Input: 2, Output: 2}}}},
			}},
		},
	}}
	provider, err := ComposeModelProvider("custom", base, config)
	if err != nil {
		t.Fatal(err)
	}
	models := provider.GetAllModels()
	if len(models) != 1 {
		t.Fatalf("models = %+v", models)
	}
	model := models[0]
	if model.PromptCache[ai.CacheRetentionShort] != int(^uint(0)>>1) || len(model.Cost.Tiers) != 1 ||
		model.Cost.Tiers[0].InputTokensAbove != int64(^uint64(0)>>1) {
		t.Fatalf("model = %+v, want large positive config values saturated at their integer bounds", model)
	}
}

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
