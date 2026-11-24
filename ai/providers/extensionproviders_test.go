package providers

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
)

// TestExtensionProvidersRegistered covers the two user provider extensions:
// the providers register with their static catalogs and env API-key auth.
// commandCodeExcludedIDs are the ids the extension never surfaces
// (UNAVAILABLE_IDS + DOMINATED_IDS).
var commandCodeExcludedIDs = map[string]bool{
	"claude-sonnet-5":                     true,
	"claude-sonnet-4-6":                   true,
	"claude-fable-5-1":                    true,
	"claude-fable-5":                      true,
	"claude-opus-5":                       true,
	"claude-opus-4-8":                     true,
	"claude-opus-4-7":                     true,
	"claude-haiku-4-5-20251001":           true,
	"gpt-5.6-terra":                       true,
	"gpt-5.5":                             true,
	"gpt-5.4":                             true,
	"gpt-5.3-codex":                       true,
	"gpt-5.4-mini":                        true,
	"google/gemini-3.6-flash":             true,
	"google/gemini-3.5-flash":             true,
	"google/gemini-3.5-flash-lite":        true,
	"google/gemini-3.1-flash-lite":        true,
	"sakana/fugu-ultra":                   true,
	"meta/muse-spark-1.1":                 true,
	"thinkingmachines/tinker-70b":         true,
	"zai-org/GLM-5.2-Fast":                true,
	"MiniMaxAI/MiniMax-M2.7":              true,
	"zai-org/GLM-5":                       true,
	"zai-org/GLM-5.1":                     true,
	"zai-org/GLM-5.2":                     true,
	"gpt-5.6-sol":                         true,
	"moonshotai/Kimi-K2.5":                true,
	"moonshotai/Kimi-K2.7-Code":           true,
	"moonshotai/Kimi-K2.7-Code-Highspeed": true,
	"xiaomi/mimo-v2.5":                    true,
	"thinkingmachines/inkling":            true,
	"thinkingmachines/inkling-small":      true,
	"xai/grok-4.5":                        true,
	"MiniMaxAI/MiniMax-M2.5":              true,
	"stepfun/Step-3.5-Flash":              true,
	"meta/muse-spark-1.2":                 true,
	"meta/muse-spark-1.2-contributor":     true,
	"Qwen/Qwen3.6-Max-Preview":            true,
	"Qwen/Qwen3.6-Plus":                   true,
	"Qwen/Qwen3.7-Max":                    true,
	"Qwen/Qwen3.7-Plus":                   true,
	"Qwen/Qwen3.7-Flash":                  true,
	"Qwen/Qwen3.8-Max-0902":               true,
	"moonshotai/Kimi-K2.6":                true,
	"tencent/hy3-paid":                    true,
}

func TestExtensionProvidersRegistered(t *testing.T) {
	providers := ExtensionProviders()
	if len(providers) != len(ExtensionProviderIDs) {
		t.Fatalf("providers = %d, want %d", len(providers), len(ExtensionProviderIDs))
	}
	byID := map[string]*ai.Provider{}
	for _, provider := range providers {
		byID[provider.ID] = provider
	}
	for _, id := range ExtensionProviderIDs {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing provider %q", id)
		}
	}
	hyper := byID["hyper"]
	if hyper == nil || hyper.Name != "Charm Hyper" || hyper.BaseURL != "https://hyper.charm.land/v1" {
		t.Fatalf("hyper = %+v", hyper)
	}
	if hyper.Auth.APIKey == nil {
		t.Fatalf("hyper auth = %+v", hyper.Auth)
	}
	commandCode := byID["commandcode"]
	if commandCode == nil || commandCode.Name != "Command Code" || commandCode.BaseURL != "https://api.commandcode.ai/provider/v1" {
		t.Fatalf("commandcode = %+v", commandCode)
	}
}

// TestHyperCatalog covers the Charm Hyper catalog invariants.
func TestHyperCatalog(t *testing.T) {
	models := hyperModels()
	if len(models) != 11 {
		t.Fatalf("hyper models = %d, want 11", len(models))
	}
	byID := map[string]*ai.Model{}
	for _, model := range models {
		if model.Provider != "hyper" {
			t.Errorf("provider = %q", model.Provider)
		}
		if model.API != ai.APIOpenAICompletions {
			t.Errorf("%s api = %q", model.ID, model.API)
		}
		if model.BaseURL != "https://hyper.charm.land/v1" {
			t.Errorf("%s baseUrl = %q", model.ID, model.BaseURL)
		}
		byID[model.ID] = model
	}
	flash := byID["glm-5.3-flash"]
	if flash == nil {
		t.Fatal("glm-5.3-flash missing")
	}
	if flash.ContextWindow != 1_048_576 || flash.MaxTokens != 131_072 {
		t.Fatalf("glm-5.3-flash limits = %d/%d", flash.ContextWindow, flash.MaxTokens)
	}
	if flash.Cost.Input != 0.16332 || flash.Cost.CacheRead != 0.031575 {
		t.Fatalf("glm-5.3-flash cost = %+v", flash.Cost)
	}
	if len(flash.Input) != 2 || flash.Input[1] != "image" {
		t.Fatalf("glm-5.3-flash input = %v", flash.Input)
	}
	// The effort vocabulary maps onto pi's thinking levels.
	if flash.ThinkingLevelMap[ai.ThinkLow] == nil || flash.ThinkingLevelMap[ai.ThinkMedium] != nil {
		t.Fatalf("glm-5.3-flash thinkingLevelMap = %+v", flash.ThinkingLevelMap)
	}
	if flash.Compat == nil || flash.Compat.OpenAICompletions == nil ||
		flash.Compat.OpenAICompletions.ThinkingFormat == nil ||
		*flash.Compat.OpenAICompletions.ThinkingFormat != "deepseek" {
		t.Fatalf("glm-5.3-flash compat = %+v", flash.Compat)
	}
}

// TestCommandCodeCatalog covers the Command Code routing and exclusions.
func TestCommandCodeCatalog(t *testing.T) {
	models := commandCodeModels()
	if len(models) != 25 {
		t.Fatalf("commandcode models = %d, want 25", len(models))
	}
	byID := map[string]*ai.Model{}
	for _, model := range models {
		if commandCodeExcludedIDs[model.ID] {
			t.Errorf("%s must be excluded", model.ID)
		}
		if model.Provider != "commandcode" {
			t.Errorf("provider = %q", model.Provider)
		}
		byID[model.ID] = model
	}
	// Claude models route to anthropic-messages at the base URL minus /v1.
	claude, ok := byID["claude-haiku-4-5-20251001"]
	if ok {
		if claude.API != ai.APIAnthropicMessages || claude.BaseURL != "https://api.commandcode.ai/provider" {
			t.Fatalf("claude routing = %s %s", claude.API, claude.BaseURL)
		}
	}
	openai := byID["deepseek/deepseek-v4-pro"]
	if openai == nil || openai.API != ai.APIOpenAICompletions || openai.BaseURL != "https://api.commandcode.ai/provider/v1" {
		t.Fatalf("openai routing = %+v", openai)
	}
	// Unavailable and dominated ids stay out.
	for _, id := range []string{"claude-sonnet-5", "zai-org/GLM-5.2-Fast", "Qwen/Qwen3.7-Flash", "moonshotai/Kimi-K2.6"} {
		if byID[id] != nil {
			t.Errorf("%s must be excluded", id)
		}
	}
	// Effort mapping: an entry with efforts keeps them, one without stays nil.
	if withEfforts := byID["deepseek/deepseek-v4-pro"]; withEfforts != nil &&
		withEfforts.ThinkingLevelMap[ai.ThinkHigh] == nil {
		t.Fatalf("efforts map = %+v", withEfforts.ThinkingLevelMap)
	}
	if noEfforts := byID["moonshotai/Kimi-K2.6"]; noEfforts == nil {
		t.Log("Kimi-K2.6 excluded; skipping the no-efforts assertion")
	}
}

// Ground truth: ~/.pi/agent/extensions/providers/hyper/catalog.ts and
// commandcode/catalog.ts (the sources this file ports). Both build models from
// a compact record whose compat object names only the fields that differ from
// detection:
//
//   - hyper's COMPAT_REASONING/COMPAT_NOREASON set supportsStore,
//     supportsReasoningEffort, thinkingFormat "deepseek" and maxTokensField,
//     and omit supportsDeveloperRole, so upstream's detectCompat applies. Since
//     v1.6.0 detection turns the developer role on only for OpenAI's own API
//     (#10741), and this provider's base URL is not it, the role stays off.
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
		if compat.SupportsDeveloperRole {
			t.Errorf("%s: supportsDeveloperRole = true; the extension omits it and detection keeps it off outside api.openai.com", model.ID)
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

// extensionCatalogEntry is one record of the extension's compact catalogue
// (~/.pi/agent/extensions/providers/{hyper,commandcode}/catalog.ts).
type extensionCatalogEntry struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Reasoning     bool           `json:"reasoning"`
	Vision        bool           `json:"vision"`
	Efforts       []string       `json:"efforts"`
	Cost          map[string]any `json:"cost"`
	ContextWindow int64          `json:"contextWindow"`
	MaxTokens     int64          `json:"maxTokens"`
}

func extensionCatalogRate(cost map[string]any, key string) float64 {
	if value, ok := cost[key].(float64); ok {
		return value
	}
	return -1
}

// TestExtensionCatalogMatchesTheSources pins both ported extension catalogues
// against the extension sources they were translated from, rather than against
// the pinned ./pi. The fixture holds the extracted CATALOG arrays and the
// UNAVAILABLE_IDS/DOMINATED_IDS union; the generators are recorded in
// testdata/extensions/README.md.
func TestExtensionCatalogMatchesTheSources(t *testing.T) {
	raw, err := os.ReadFile("testdata/extensions/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Catalogs map[string][]extensionCatalogEntry `json:"catalogs"`
		Excluded []string                           `json:"excluded"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	excluded := map[string]bool{}
	for _, id := range fixture.Excluded {
		excluded[id] = true
	}

	check := func(label string, want []extensionCatalogEntry, got []*ai.Model, skipExcluded bool) {
		wantByID := map[string]extensionCatalogEntry{}
		for _, entry := range want {
			if skipExcluded && excluded[entry.ID] {
				continue
			}
			wantByID[entry.ID] = entry
		}
		gotByID := map[string]*ai.Model{}
		for _, model := range got {
			gotByID[model.ID] = model
		}
		for id, entry := range wantByID {
			model := gotByID[id]
			if model == nil {
				t.Errorf("%s: model %q missing", label, id)
				continue
			}
			if model.Name != entry.Name || model.Reasoning != entry.Reasoning {
				t.Errorf("%s %q: name/reasoning = %q/%v, want %q/%v",
					label, id, model.Name, model.Reasoning, entry.Name, entry.Reasoning)
			}
			wantVision := 1
			if entry.Vision {
				wantVision = 2
			}
			if model.ContextWindow != entry.ContextWindow || model.MaxTokens != entry.MaxTokens || len(model.Input) != wantVision {
				t.Errorf("%s %q: window=%d maxTokens=%d input=%v, want %d/%d/%d modalities",
					label, id, model.ContextWindow, model.MaxTokens, model.Input,
					entry.ContextWindow, entry.MaxTokens, wantVision)
			}
			rates := model.Cost.ModelCostRates
			for _, pair := range []struct {
				name string
				got  float64
			}{{"input", rates.Input}, {"output", rates.Output}, {"cacheRead", rates.CacheRead}, {"cacheWrite", rates.CacheWrite}} {
				if want := extensionCatalogRate(entry.Cost, pair.name); pair.got != want {
					t.Errorf("%s %q: cost.%s = %v, want %v", label, id, pair.name, pair.got, want)
				}
			}
			if !entry.Reasoning {
				continue
			}
			// toModel maps each supported effort to itself and every other level
			// to null.
			for _, level := range []ai.ThinkingLevel{ai.ThinkOff, ai.ThinkMinimal, ai.ThinkLow, ai.ThinkMedium, ai.ThinkHigh, ai.ThinkXHigh, ai.ThinkMax} {
				var want *string
				for _, effort := range entry.Efforts {
					if effort == string(level) {
						value := string(level)
						want = &value
					}
				}
				got := model.ThinkingLevelMap[level]
				if (got == nil) != (want == nil) || (got != nil && want != nil && *got != *want) {
					t.Errorf("%s %q: thinkingLevelMap.%s = %v, want %v", label, id, level, got, want)
				}
			}
		}
		for id := range gotByID {
			if _, ok := wantByID[id]; !ok {
				t.Errorf("%s: model %q has no source entry", label, id)
			}
		}
	}
	check("hyper", fixture.Catalogs["hyper"], hyperModels(), false)
	check("commandcode", fixture.Catalogs["commandcode"], commandCodeModels(), true)

	// EXCLUDED_IDS is UNAVAILABLE_IDS plus DOMINATED_IDS, and nothing else.
	for id := range commandCodeExcludedIDs {
		if !excluded[id] {
			t.Errorf("commandCodeExcludedIDs lists %q, absent from the sources", id)
		}
	}
	for _, id := range fixture.Excluded {
		if !commandCodeExcludedIDs[id] {
			t.Errorf("the sources exclude %q, missing from commandCodeExcludedIDs", id)
		}
	}
}
