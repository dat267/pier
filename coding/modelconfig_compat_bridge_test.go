package coding

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/ai"
)

// The models.json compat bridge mirrors pi's provider compat schema field by
// field, so a key the bridge struct lacks is dropped without a warning and the
// provider silently keeps its default behavior. These keys exist in both pi
// v1.5.2 (b4afdddf) providers/compat-schema.ts and the port's ai compat structs,
// but were missing from the bridge.
func TestModelsJSONForwardsEveryCompatSchemaFlag(t *testing.T) {
	desired := "thinking_token_budget"
	cases := []struct {
		name   string
		api    string
		compat string
		check  func(t *testing.T, compat *ai.ModelCompat)
	}{
		{
			name: "completions",
			api:  string(ai.APIOpenAICompletions),
			compat: `{"zaiToolStream": true, "thinkingTokenBudgetField": "thinking_token_budget",
				"supportsThinkingTokenBudget": true, "supportsMidConvoSystemMessages": true,
				"supportsMidConvoToolAdditions": true}`,
			check: func(t *testing.T, compat *ai.ModelCompat) {
				c := compat.OpenAICompletions
				if c == nil {
					t.Fatal("no completions compat")
				}
				if c.ZaiToolStream == nil || !*c.ZaiToolStream {
					t.Fatalf("zaiToolStream = %v", c.ZaiToolStream)
				}
				if c.ThinkingTokenBudgetField == nil || *c.ThinkingTokenBudgetField != desired {
					t.Fatalf("thinkingTokenBudgetField = %v", c.ThinkingTokenBudgetField)
				}
				if c.SupportsThinkingTokenBudget == nil || !*c.SupportsThinkingTokenBudget {
					t.Fatalf("supportsThinkingTokenBudget = %v", c.SupportsThinkingTokenBudget)
				}
				if c.SupportsMidConvoSystemMessages == nil || !*c.SupportsMidConvoSystemMessages {
					t.Fatalf("supportsMidConvoSystemMessages = %v", c.SupportsMidConvoSystemMessages)
				}
				if c.SupportsMidConvoToolAdditions == nil || !*c.SupportsMidConvoToolAdditions {
					t.Fatalf("supportsMidConvoToolAdditions = %v", c.SupportsMidConvoToolAdditions)
				}
			},
		},
		{
			name: "responses",
			api:  string(ai.APIOpenAIResponses),
			compat: `{"supportsMidConvoSystemMessages": true, "supportsAdditionalTools": true,
				"supportsToolSearch": true, "supportsExplicitPromptCacheMode": true}`,
			check: func(t *testing.T, compat *ai.ModelCompat) {
				c := compat.OpenAIResponses
				if c == nil {
					t.Fatal("no responses compat")
				}
				if c.SupportsMidConvoSystemMessages == nil || !*c.SupportsMidConvoSystemMessages {
					t.Fatalf("supportsMidConvoSystemMessages = %v", c.SupportsMidConvoSystemMessages)
				}
				if c.SupportsAdditionalTools == nil || !*c.SupportsAdditionalTools {
					t.Fatalf("supportsAdditionalTools = %v", c.SupportsAdditionalTools)
				}
				if c.SupportsToolSearch == nil || !*c.SupportsToolSearch {
					t.Fatalf("supportsToolSearch = %v", c.SupportsToolSearch)
				}
				if c.SupportsExplicitPromptCacheMode == nil || !*c.SupportsExplicitPromptCacheMode {
					t.Fatalf("supportsExplicitPromptCacheMode = %v", c.SupportsExplicitPromptCacheMode)
				}
			},
		},
		{
			name:   "anthropic",
			api:    string(ai.APIAnthropicMessages),
			compat: `{"supportsMidConvoSystemMessages": true, "supportsMidConvoToolChanges": true}`,
			check: func(t *testing.T, compat *ai.ModelCompat) {
				c := compat.AnthropicMessages
				if c == nil {
					t.Fatal("no anthropic compat")
				}
				if c.SupportsMidConvoSystemMessages == nil || !*c.SupportsMidConvoSystemMessages {
					t.Fatalf("supportsMidConvoSystemMessages = %v", c.SupportsMidConvoSystemMessages)
				}
				if c.SupportsMidConvoToolChanges == nil || !*c.SupportsMidConvoToolChanges {
					t.Fatalf("supportsMidConvoToolChanges = %v", c.SupportsMidConvoToolChanges)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "models.json")
			content := `{"providers": {"custom": {"baseUrl": "https://custom.example.com/v1", "api": "` +
				tc.api + `", "compat": ` + tc.compat + `, "models": [{"id": "m", "name": "M"}]}}}`
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			config := LoadModelConfig(path)
			if err := config.GetError(); err != "" {
				t.Fatalf("config: %s", err)
			}
			provider := config.GetProvider("custom")
			if provider == nil {
				t.Fatal("provider missing")
			}
			models, err := applyModelsJSON("custom", nil, provider)
			if err != nil {
				t.Fatal(err)
			}
			if len(models) != 1 || models[0].Compat == nil {
				t.Fatalf("models = %+v", models)
			}
			tc.check(t, models[0].Compat)
		})
	}
}
