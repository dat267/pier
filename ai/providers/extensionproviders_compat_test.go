package providers

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// Ground truth: ~/.pi/agent/extensions/providers/hyper/catalog.ts and
// commandcode/catalog.ts (the sources this file ports). Both build models from
// a compact record whose compat object names only the fields that differ from
// detection:
//
//   - hyper's COMPAT_REASONING/COMPAT_NOREASON set supportsStore,
//     supportsReasoningEffort, thinkingFormat "deepseek" and maxTokensField,
//     and omit supportsDeveloperRole, so upstream's detectCompat applies. The
//     provider is not one of its non-standard ones and is not OpenRouter, so
//     detection turns the developer role on.
//   - commandcode's COMPAT_OPENAI_* set supportsStore, supportsDeveloperRole,
//     supportsReasoningEffort and maxTokensField, and omit thinkingFormat, so
//     detection applies: its base URL matches none of the special cases and
//     the format stays "openai".
//
// A compat field the extension leaves out must therefore stay out of the model,
// or the request body diverges from what the extension sends.
func TestExtensionProviderCompatMatchesTheExtensionCatalog(t *testing.T) {
	hyper := HyperProvider()
	for _, model := range hyper.GetModels() {
		compat := ai.GetOpenAICompletionsCompat(model)
		if !compat.SupportsDeveloperRole {
			t.Errorf("%s: supportsDeveloperRole = false; the extension omits it and detection enables it", model.ID)
		}
		if compat.ThinkingFormat != "deepseek" {
			t.Errorf("%s: thinkingFormat = %q; the extension sets deepseek", model.ID, compat.ThinkingFormat)
		}
		if compat.SupportsStore {
			t.Errorf("%s: supportsStore = true; the extension sets false", model.ID)
		}
		if compat.MaxTokensField != "max_tokens" {
			t.Errorf("%s: maxTokensField = %q; the extension sets max_tokens", model.ID, compat.MaxTokensField)
		}
	}
	commandCode := CommandCodeProvider()
	for _, model := range commandCode.GetModels() {
		if model.API != ai.APIOpenAICompletions {
			continue
		}
		compat := ai.GetOpenAICompletionsCompat(model)
		if compat.ThinkingFormat != "openai" {
			t.Errorf("%s: thinkingFormat = %q; the extension omits it and detection keeps openai", model.ID, compat.ThinkingFormat)
		}
		if compat.SupportsDeveloperRole {
			t.Errorf("%s: supportsDeveloperRole = true; the extension sets false", model.ID)
		}
	}
}
