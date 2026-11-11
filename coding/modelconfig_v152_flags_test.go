package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.5.2 (b4afdddf) providers/compat-schema.ts added three compat flags. A
// models.json entry that sets them must reach the resolved provider compat, or
// the flag is silently ignored.
func TestCompatFromConfigCarriesV152Flags(t *testing.T) {
	yes := true
	disabled := "disabled"
	compat := &ModelsJSONCompat{
		RequiresNonNullAssistantContent: &yes,
		RequiresToolsWithToolHistory:    &yes,
		PromptCacheKeyMode:              &disabled,
	}
	for _, api := range []ai.Api{ai.APIOpenAICompletions, ai.APIOpenAIResponses} {
		t.Run(string(api), func(t *testing.T) {
			resolved := compatFromConfig(compat, api)
			if resolved == nil {
				t.Fatal("compatFromConfig returned nil")
			}
			var mode *string
			switch api {
			case ai.APIOpenAICompletions:
				if resolved.OpenAICompletions == nil {
					t.Fatal("openai completions compat missing")
				}
				mode = resolved.OpenAICompletions.PromptCacheKeyMode
			case ai.APIOpenAIResponses:
				if resolved.OpenAIResponses == nil {
					t.Fatal("openai responses compat missing")
				}
				mode = resolved.OpenAIResponses.PromptCacheKeyMode
			}
			if mode == nil || *mode != "disabled" {
				t.Fatalf("promptCacheKeyMode = %v, want disabled", mode)
			}
			if api == ai.APIOpenAICompletions {
				openai := resolved.OpenAICompletions
				if openai.RequiresNonNullAssistantContent == nil || !*openai.RequiresNonNullAssistantContent {
					t.Fatal("requiresNonNullAssistantContent did not survive the config")
				}
				if openai.RequiresToolsWithToolHistory == nil || !*openai.RequiresToolsWithToolHistory {
					t.Fatal("requiresToolsWithToolHistory did not survive the config")
				}
			}
		})
	}
}

// The union schema only accepts the three documented modes.
func TestValidateCompatAcceptsOnlyDocumentedPromptCacheKeyModes(t *testing.T) {
	for _, mode := range []string{"auto", "enabled", "disabled"} {
		var errors []validationError
		validateCompat(map[string]any{"promptCacheKeyMode": mode}, "model.compat", &errors)
		if len(errors) != 0 {
			t.Fatalf("mode %q rejected: %+v", mode, errors)
		}
	}
	var errors []validationError
	validateCompat(map[string]any{"promptCacheKeyMode": "sometimes"}, "model.compat", &errors)
	if len(errors) != 1 || errors[0].path != "model.compat.promptCacheKeyMode" {
		t.Fatalf("errors = %+v", errors)
	}
	for _, field := range []string{"requiresNonNullAssistantContent", "requiresToolsWithToolHistory"} {
		var boolErrors []validationError
		validateCompat(map[string]any{field: true}, "model.compat", &boolErrors)
		if len(boolErrors) != 0 {
			t.Fatalf("%s rejected: %+v", field, boolErrors)
		}
		var wrongType []validationError
		validateCompat(map[string]any{field: "yes"}, "model.compat", &wrongType)
		if len(wrongType) != 1 {
			t.Fatalf("%s accepted a string: %+v", field, wrongType)
		}
	}
}
