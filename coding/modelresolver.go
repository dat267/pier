package coding

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dat267/pier/ai"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Port of core/model-resolver.ts plus the thinking-level validation from
// core/defaults.ts and cli/args.ts.

// DefaultModelPerProvider maps each known provider to its default model id
// (upstream defaultModelPerProvider).
var DefaultModelPerProvider = map[ai.ProviderId]string{
	"amazon-bedrock":             "us.anthropic.claude-opus-4-6-v1",
	"ant-ling":                   "Ring-2.6-1T",
	"anthropic":                  "claude-opus-4-8",
	"openai":                     "gpt-5.5",
	"azure":                      "gpt-5.4",
	"openai-codex":               "gpt-6.1-sol",
	"radius":                     "balanced",
	"nvidia":                     "nvidia/nemotron-3-ultra-550b-a55b",
	"deepseek":                   "deepseek-v4-pro",
	"google":                     "gemini-3.1-pro-preview",
	"google-vertex":              "gemini-3.1-pro-preview",
	"github-copilot":             "gpt-5.4",
	"openrouter":                 "moonshotai/kimi-k2.6",
	"vercel-ai-gateway":          "zai/glm-5.1",
	"xai":                        "grok-4.7",
	"groq":                       "openai/gpt-oss-120b",
	"cerebras":                   "gpt-oss-120b",
	"zai":                        "glm-5.3",
	"zai-coding-cn":              "glm-5.3",
	"mistral":                    "devstral-medium-latest",
	"minimax":                    "MiniMax-M2.7",
	"minimax-cn":                 "MiniMax-M2.7",
	"moonshotai":                 "kimi-k2.6",
	"moonshotai-cn":              "kimi-k2.6",
	"huggingface":                "moonshotai/Kimi-K2.6",
	"fireworks":                  "accounts/fireworks/models/kimi-k3",
	"together":                   "moonshotai/Kimi-K3",
	"baseten":                    "zai-org/GLM-5.2",
	"opencode":                   "kimi-k2.6",
	"opencode-go":                "kimi-k3",
	"kimi-coding":                "kimi-for-coding",
	"meta":                       "muse-spark-1.3",
	"cloudflare-workers-ai":      "@cf/moonshotai/kimi-k2.6",
	"cloudflare-ai-gateway":      "workers-ai/@cf/moonshotai/kimi-k2.6",
	"qwen-token-plan":            "qwen3.7-max",
	"qwen-token-plan-cn":         "qwen3.7-max",
	"qwen-token-plan-individual": "qwen3.8-max",
	"xiaomi":                     "mimo-v2.5-pro",
	"xiaomi-token-plan-cn":       "mimo-v2.5-pro",
	"xiaomi-token-plan-ams":      "mimo-v2.5-pro",
	"xiaomi-token-plan-sgp":      "mimo-v2.5-pro",
}

// IsValidThinkingLevel reports whether a string is a selectable thinking level.
func IsValidThinkingLevel(level string) bool {
	for _, candidate := range ThinkingLevelOptions {
		if string(candidate) == level {
			return true
		}
	}
	return false
}

// ScopedModel is one resolved model pattern.
type ScopedModel struct {
	Model *ai.Model
	// ThinkingLevel is set when the pattern carried one (e.g. "model:high").
	ThinkingLevel ai.ThinkingLevel
	HasThinking   bool
}

// IsAlias reports whether a model id looks like an alias (no date suffix).
func IsAlias(id string) bool {
	if strings.HasSuffix(id, "-latest") {
		return true
	}
	// A date suffix is -YYYYMMDD.
	if len(id) >= 9 && id[len(id)-9] == '-' {
		digits := id[len(id)-8:]
		allDigits := true
		for _, char := range digits {
			if char < '0' || char > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return false
		}
	}
	return true
}

// ModelReferenceRequest is one model reference to resolve
// (upstream ModelReferenceRequest).
type ModelReferenceRequest struct {
	// Text is a bare model id or `provider/id`, optionally `:<thinking>`.
	Text string
	// Provider restricts the pool to one provider.
	Provider string
	// ThinkingSuffix is "none", "strict" or "lenient".
	ThinkingSuffix string
	// Fuzzy matches id and name substrings when no exact match exists.
	Fuzzy bool
}

// ModelReferenceTarget is set on a not-found result when a provider is known,
// for building a custom model id.
type ModelReferenceTarget struct {
	Provider       string
	ProviderModels []*ai.Model
	// ModelText is the text after the provider prefix, including any suffix.
	ModelText string
	// ModelID is ModelText without a valid thinking suffix.
	ModelID       string
	ThinkingLevel ai.ThinkingLevel
	HasThinking   bool
}

// ModelReferenceResult is a resolution result (upstream ModelReferenceResult).
type ModelReferenceResult struct {
	Status        string // "resolved" | "ambiguous" | "not-found"
	Model         *ai.Model
	ThinkingLevel ai.ThinkingLevel
	HasThinking   bool
	Warning       string
	Candidates    []*ai.Model
	UsableCount   int
	Target        *ModelReferenceTarget
}

const (
	thinkingSuffixNone    = "none"
	thinkingSuffixStrict  = "strict"
	thinkingSuffixLenient = "lenient"
)

func splitThinkingSuffix(text string) (string, ai.ThinkingLevel, bool) {
	colonIndex := strings.LastIndex(text, ":")
	if colonIndex == -1 {
		return "", "", false
	}
	suffix := text[colonIndex+1:]
	if !IsValidThinkingLevel(suffix) {
		return "", "", false
	}
	return text[:colonIndex], ai.ThinkingLevel(suffix), true
}

func splitProviderPrefix(text string, models []*ai.Model) (string, string, bool) {
	slashIndex := strings.Index(text, "/")
	if slashIndex == -1 {
		return "", "", false
	}
	prefix := ai.JSLower(ai.JSTrim(text[:slashIndex]))
	modelText := ai.JSTrim(text[slashIndex+1:])
	if prefix == "" || modelText == "" {
		return "", "", false
	}
	for _, model := range models {
		if ai.JSLower(model.Provider) == prefix {
			return model.Provider, modelText, true
		}
	}
	return "", "", false
}

// ResolveModelReference resolves one reference. usable only breaks ties between
// providers, so a unique match resolves even without credentials (`--api-key`
// first-time setup).
func ResolveModelReference(request ModelReferenceRequest, models []*ai.Model, usable func(string) bool) ModelReferenceResult {
	text := ai.JSTrim(request.Text)
	if text == "" {
		return ModelReferenceResult{Status: "not-found"}
	}
	constraint := ai.JSLower(request.Provider)
	pool := models
	if constraint != "" {
		pool = nil
		for _, model := range models {
			if ai.JSLower(model.Provider) == constraint {
				pool = append(pool, model)
			}
		}
	}
	result := resolveReferenceText(text, request, pool, usable)
	if result.Status != "not-found" {
		return result
	}

	provider, modelText, splitOK := splitProviderPrefix(text, pool)
	var anchor *ai.Model
	if constraint != "" {
		if len(pool) > 0 {
			anchor = pool[0]
		}
	} else if splitOK {
		for _, model := range pool {
			if model.Provider == provider {
				anchor = model
				break
			}
		}
	}
	if anchor == nil {
		return result
	}
	if !splitOK {
		modelText = text
	}
	target := &ModelReferenceTarget{Provider: anchor.Provider, ModelText: modelText, ModelID: modelText}
	for _, model := range pool {
		if model.Provider == anchor.Provider {
			target.ProviderModels = append(target.ProviderModels, model)
		}
	}
	if request.ThinkingSuffix != thinkingSuffixNone {
		if base, level, ok := splitThinkingSuffix(modelText); ok {
			target.ModelID = base
			target.ThinkingLevel = level
			target.HasThinking = true
		}
	}
	return ModelReferenceResult{Status: "not-found", Target: target}
}

// FindExactModelReferenceMatch finds a model by bare id or canonical
// provider/model reference; ambiguous bare ids are rejected and no thinking
// suffix is parsed.
func FindExactModelReferenceMatch(modelReference string, models []*ai.Model) *ai.Model {
	result := ResolveModelReference(ModelReferenceRequest{Text: modelReference, ThinkingSuffix: thinkingSuffixNone}, models, func(string) bool { return true })
	if result.Status == "resolved" {
		return result.Model
	}
	return nil
}

func resolveReferenceText(text string, request ModelReferenceRequest, pool []*ai.Model, usable func(string) bool) ModelReferenceResult {
	if reference, ok := selectReference(text, pool, request.Fuzzy, usable); ok {
		return reference
	}
	if request.Fuzzy {
		if fuzzy, ok := selectFuzzy(fuzzyMatches(text, pool), usable); ok {
			return fuzzy
		}
	}
	if request.ThinkingSuffix == thinkingSuffixNone {
		return ModelReferenceResult{Status: "not-found"}
	}
	if base, level, ok := splitThinkingSuffix(text); ok {
		inner := resolveReferenceText(base, request, pool, usable)
		if inner.Status == "not-found" {
			return inner
		}
		if inner.Warning != "" {
			inner.ThinkingLevel, inner.HasThinking = "", false
		} else {
			inner.ThinkingLevel, inner.HasThinking = level, true
		}
		return inner
	}
	// Keep an invalid suffix as part of the id and fail, rather than resolve a
	// different model.
	if request.ThinkingSuffix == thinkingSuffixStrict {
		return ModelReferenceResult{Status: "not-found"}
	}
	colonIndex := strings.LastIndex(text, ":")
	if colonIndex == -1 {
		return ModelReferenceResult{Status: "not-found"}
	}
	suffix := text[colonIndex+1:]
	inner := resolveReferenceText(text[:colonIndex], request, pool, usable)
	if inner.Status == "not-found" {
		return inner
	}
	inner.ThinkingLevel, inner.HasThinking = "", false
	inner.Warning = fmt.Sprintf("Invalid thinking level %q in pattern %q. Using default instead.", suffix, text)
	return inner
}

// selectReference: `provider/rest` is only a hint. The named provider wins when
// usable, since that is what the syntax usually means; otherwise a sole usable
// raw match wins. An unusable named match is still preferred over unusable raw
// matches, so `--api-key` can set up the named provider.
func selectReference(text string, pool []*ai.Model, fuzzy bool, usable func(string) bool) (ModelReferenceResult, bool) {
	lowerText := ai.JSLower(text)
	var raw []*ai.Model
	for _, model := range pool {
		if ai.JSLower(model.ID) == lowerText {
			raw = append(raw, model)
		}
	}
	namedProvider, modelText, splitOK := splitProviderPrefix(text, pool)
	var named *ai.Model
	if splitOK {
		var providerModels []*ai.Model
		for _, model := range pool {
			if model.Provider == namedProvider {
				providerModels = append(providerModels, model)
			}
		}
		lowerModelText := ai.JSLower(modelText)
		for _, model := range providerModels {
			if ai.JSLower(model.ID) == lowerModelText {
				named = model
				break
			}
		}
		if named == nil {
			for _, model := range raw {
				if model.Provider == namedProvider {
					named = model
					break
				}
			}
		}
		if named == nil && fuzzy {
			named = bestFuzzyMatch(fuzzyMatches(modelText, providerModels))
		}
	}

	var others []*ai.Model
	for _, model := range raw {
		if model != named {
			others = append(others, model)
		}
	}
	candidates := others
	if named != nil {
		candidates = append([]*ai.Model{named}, others...)
	}
	if len(candidates) == 0 {
		return ModelReferenceResult{}, false
	}
	if providerSetSize(candidates) == 1 {
		return resolvesTo(candidates[0]), true
	}
	if named != nil && usable(named.Provider) {
		return resolvesTo(named), true
	}
	var usableOthers []*ai.Model
	for _, model := range others {
		if usable(model.Provider) {
			usableOthers = append(usableOthers, model)
		}
	}
	if len(usableOthers) == 1 {
		return resolvesTo(usableOthers[0]), true
	}
	if len(usableOthers) == 0 && named != nil {
		return resolvesTo(named), true
	}
	return ModelReferenceResult{Status: "ambiguous", Candidates: candidates, UsableCount: len(usableOthers)}, true
}

func fuzzyMatches(text string, models []*ai.Model) []*ai.Model {
	lowerText := ai.JSLower(text)
	var matches []*ai.Model
	for _, model := range models {
		if strings.Contains(ai.JSLower(model.ID), lowerText) ||
			(model.Name != "" && strings.Contains(ai.JSLower(model.Name), lowerText)) {
			matches = append(matches, model)
		}
	}
	return matches
}

func selectFuzzy(matches []*ai.Model, usable func(string) bool) (ModelReferenceResult, bool) {
	if len(matches) == 0 {
		return ModelReferenceResult{}, false
	}
	multipleProviders := providerSetSize(matches) > 1
	var usableMatches []*ai.Model
	if multipleProviders {
		for _, model := range matches {
			if usable(model.Provider) {
				usableMatches = append(usableMatches, model)
			}
		}
	}
	pool := matches
	if len(usableMatches) > 0 {
		pool = usableMatches
	}
	best := bestFuzzyMatch(pool)
	if best == nil {
		return ModelReferenceResult{}, false
	}
	var tied []*ai.Model
	for _, model := range pool {
		if model.ID == best.ID {
			tied = append(tied, model)
		}
	}
	if providerSetSize(tied) == 1 {
		return resolvesTo(best), true
	}
	usableCount := 0
	if len(usableMatches) > 0 {
		usableCount = len(tied)
	}
	return ModelReferenceResult{Status: "ambiguous", Candidates: tied, UsableCount: usableCount}, true
}

// bestFuzzyMatch prefers aliases over dated versions, then the latest.
func bestFuzzyMatch(matches []*ai.Model) *ai.Model {
	if len(matches) == 0 {
		return nil
	}
	var aliases []*ai.Model
	for _, model := range matches {
		if IsAlias(model.ID) {
			aliases = append(aliases, model)
		}
	}
	pool := matches
	if len(aliases) > 0 {
		pool = aliases
	}
	sorted := append([]*ai.Model{}, pool...)
	// D257: use root collation, like the file listing, rather than byte
	// order or the host locale (Swedish sorts model-ä after model-z).
	collator := collate.New(language.Und)
	sort.SliceStable(sorted, func(i, j int) bool { return collator.CompareString(sorted[i].ID, sorted[j].ID) > 0 })
	return sorted[0]
}

func providerSetSize(models []*ai.Model) int {
	set := map[string]struct{}{}
	for _, model := range models {
		set[model.Provider] = struct{}{}
	}
	return len(set)
}

func resolvesTo(model *ai.Model) ModelReferenceResult {
	return ModelReferenceResult{Status: "resolved", Model: model}
}

// buildFallbackModel clones a provider's default model under a custom id.
func buildFallbackModel(providerModels []*ai.Model, modelID string) *ai.Model {
	if len(providerModels) == 0 {
		return nil
	}
	baseModel := providerModels[0]
	if defaultID := DefaultModelPerProvider[ai.ProviderId(baseModel.Provider)]; defaultID != "" {
		for _, model := range providerModels {
			if model.ID == defaultID {
				baseModel = model
				break
			}
		}
	}
	cloned := *baseModel
	cloned.ID = modelID
	cloned.Name = modelID
	return &cloned
}

// ModelScopeDiagnostic is one scope warning.
type ModelScopeDiagnostic struct {
	Type    string // "warning"
	Code    string // "no-match" | "invalid-thinking-level"
	Message string
	Pattern string
}

// ResolveModelScopeResult is the scope resolution result.
type ResolveModelScopeResult struct {
	ScopedModels []ScopedModel
	Diagnostics  []ModelScopeDiagnostic
}

// ResolveModelScopeFromModels resolves model patterns over a model list.
func ResolveModelScopeFromModels(patterns []string, models []*ai.Model) ResolveModelScopeResult {
	var scopedModels []ScopedModel
	var diagnostics []ModelScopeDiagnostic
	add := func(model *ai.Model, thinking ai.ThinkingLevel, hasThinking bool) {
		for _, scoped := range scopedModels {
			if ai.ModelsAreEqual(scoped.Model, model) {
				return
			}
		}
		scopedModels = append(scopedModels, ScopedModel{Model: model, ThinkingLevel: thinking, HasThinking: hasThinking})
	}
	noMatch := func(pattern string) {
		diagnostics = append(diagnostics, ModelScopeDiagnostic{
			Type: "warning", Code: "no-match", Message: fmt.Sprintf("No models match pattern %q", pattern), Pattern: pattern,
		})
	}
	ambiguous := func(pattern string, candidates []*ai.Model) {
		set := map[string]struct{}{}
		for _, model := range candidates {
			set[model.Provider] = struct{}{}
		}
		var providers []string
		for provider := range set {
			providers = append(providers, provider)
		}
		sort.Strings(providers)
		diagnostics = append(diagnostics, ModelScopeDiagnostic{
			Type: "warning", Code: "ambiguous", Pattern: pattern,
			Message: fmt.Sprintf("Pattern %q matches several providers: %s. Use provider/id to choose one.", pattern, strings.Join(providers, ", ")),
		})
	}

	for _, pattern := range patterns {
		// Check if pattern contains glob characters.
		if strings.ContainsAny(pattern, "*?[") {
			// Extract an optional thinking level suffix (e.g. "provider/*:high").
			globPattern := pattern
			var thinkingLevel ai.ThinkingLevel
			hasThinking := false
			if base, level, ok := splitThinkingSuffix(pattern); ok {
				globPattern = base
				thinkingLevel = level
				hasThinking = true
			}

			// Ids such as `bracketed-model[1m]` are not globs.
			exact := ResolveModelReference(ModelReferenceRequest{Text: globPattern, ThinkingSuffix: thinkingSuffixNone}, models, func(string) bool { return true })
			var matches []*ai.Model
			switch exact.Status {
			case "resolved":
				matches = []*ai.Model{exact.Model}
			case "ambiguous":
				matches = exact.Candidates
				ambiguous(pattern, matches)
			default:
				// Match against "provider/modelId" format OR just model ID.
				for _, model := range models {
					if MatchGlob(globPattern, model.Provider+"/"+model.ID, true) || MatchGlob(globPattern, model.ID, true) {
						matches = append(matches, model)
					}
				}
			}
			if len(matches) == 0 {
				noMatch(pattern)
			}
			for _, model := range matches {
				add(model, thinkingLevel, hasThinking)
			}
			continue
		}

		result := ResolveModelReference(ModelReferenceRequest{Text: pattern, ThinkingSuffix: thinkingSuffixLenient, Fuzzy: true}, models, func(string) bool { return true })
		if result.Status == "not-found" {
			noMatch(pattern)
			continue
		}
		if result.Warning != "" {
			diagnostics = append(diagnostics, ModelScopeDiagnostic{
				Type: "warning", Code: "invalid-thinking-level", Message: result.Warning, Pattern: pattern,
			})
		}
		matches := result.Candidates
		if result.Status == "resolved" {
			matches = []*ai.Model{result.Model}
		} else {
			ambiguous(pattern, result.Candidates)
		}
		for _, model := range matches {
			add(model, result.ThinkingLevel, result.HasThinking)
		}
	}
	return ResolveModelScopeResult{ScopedModels: scopedModels, Diagnostics: diagnostics}
}

// ResolveCliModelResult is the CLI model resolution result.
type ResolveCliModelResult struct {
	Model         *ai.Model
	ThinkingLevel ai.ThinkingLevel
	HasThinking   bool
	Warning       string
	// Error is a CLI-facing error; when set the model is nil.
	Error string
}

// ResolveCliModelOptions configure ResolveCliModel.
type ResolveCliModelOptions struct {
	CLIProvider    string
	CLIModel       string
	CLIThinking    ai.ThinkingLevel
	HasCLIThinking bool
	// InlineAPIKey is set when --api-key was supplied for the selected provider;
	// stored credentials must not choose the provider then.
	InlineAPIKey bool
	ModelRuntime ModelRuntimeSource
}

// ResolveCliModel resolves a single model from CLI flags.
func ResolveCliModel(options ResolveCliModelOptions) ResolveCliModelResult {
	cliProvider := options.CLIProvider
	cliModel := options.CLIModel
	modelRuntime := options.ModelRuntime

	if cliModel == "" {
		return ResolveCliModelResult{}
	}

	// Important: use *all* models here, not just models with pre-configured auth.
	// This allows "--api-key" to be used for first-time setup.
	models := modelRuntime.GetModels("")
	if len(models) == 0 {
		return ResolveCliModelResult{Error: "No models available. Check your installation or add models to models.json."}
	}

	provider := ""
	if cliProvider != "" {
		for _, model := range models {
			if ai.JSLower(model.Provider) == ai.JSLower(cliProvider) {
				provider = model.Provider
				break
			}
		}
		if provider == "" {
			return ResolveCliModelResult{Error: fmt.Sprintf("Unknown provider %q. Use --list-models to see available providers/models.", cliProvider)}
		}
	}

	usable := func(providerID string) bool { return modelRuntime.HasConfiguredAuth(providerID) }
	if options.InlineAPIKey {
		// An inline key goes to whichever provider is selected, so stored
		// credentials must not choose it.
		usable = func(string) bool { return false }
	}
	result := ResolveModelReference(ModelReferenceRequest{
		Text: cliModel, Provider: provider, ThinkingSuffix: thinkingSuffixStrict, Fuzzy: true,
	}, models, usable)
	if result.Status == "resolved" {
		return ResolveCliModelResult{
			Model: result.Model, ThinkingLevel: result.ThinkingLevel, HasThinking: result.HasThinking, Warning: result.Warning,
		}
	}
	if result.Status == "ambiguous" {
		var described []string
		for _, model := range result.Candidates {
			described = append(described, model.Provider+"/"+model.ID)
		}
		sort.Strings(described)
		authHint := ""
		switch {
		case options.InlineAPIKey:
			authHint = "--api-key does not choose a provider."
		case result.UsableCount == 0:
			authHint = "No matching provider is authenticated."
		default:
			authHint = "More than one matching provider is authenticated."
		}
		return ResolveCliModelResult{Error: fmt.Sprintf(
			"Model %q is ambiguous across providers: %s. %s Use --provider or provider/model.",
			cliModel, strings.Join(described, ", "), authHint)}
	}

	target := result.Target
	if target == nil {
		return ResolveCliModelResult{Error: fmt.Sprintf("Model %q not found. Use --list-models to see available models.", cliModel)}
	}

	// Use the thinking level suffix only when --thinking is not explicitly
	// provided; otherwise it stays part of the custom model id.
	modelID := target.ModelText
	fallbackThinking := target.ThinkingLevel
	hasFallbackThinking := target.HasThinking
	if !options.HasCLIThinking {
		modelID = target.ModelID
	} else {
		fallbackThinking = ""
		hasFallbackThinking = false
	}
	fallbackModel := buildFallbackModel(target.ProviderModels, modelID)
	requestedThinking := options.CLIThinking
	if !options.HasCLIThinking {
		requestedThinking = fallbackThinking
	}
	model := fallbackModel
	if requestedThinking != "" && requestedThinking != ai.ThinkOff {
		cloned := *fallbackModel
		cloned.Reasoning = true
		model = &cloned
	}
	return ResolveCliModelResult{
		Model: model, ThinkingLevel: fallbackThinking, HasThinking: hasFallbackThinking,
		Warning: fmt.Sprintf("Model %q not found for provider %q. Using custom model id.", modelID, target.Provider),
	}
}

// InitialModelResult is the initial model selection.
//
// D21: upstream prints a CLI error and exits the process when the explicit
// --provider/--model pair cannot be resolved; the Go port reports it in Error
// so the caller decides (library code must not exit).
type InitialModelResult struct {
	Model           *ai.Model
	ThinkingLevel   ai.ThinkingLevel
	FallbackMessage string
	Error           string
}

// FindInitialModelOptions configure FindInitialModel.
type FindInitialModelOptions struct {
	CLIProvider          string
	CLIModel             string
	ScopedModels         []ScopedModel
	IsContinuing         bool
	DefaultProvider      string
	DefaultModelID       string
	DefaultThinkingLevel ai.ThinkingLevel
	HasDefaultThinking   bool
	ModelThinkingLevels  map[string]ai.ThinkingLevel
	ModelRuntime         ModelRuntimeSource
}

// FindInitialModel picks the initial model with upstream's priority order.
func FindInitialModel(options FindInitialModelOptions) InitialModelResult {
	var model *ai.Model
	thinkingLevel := DefaultThinkingLevel

	if options.CLIProvider != "" && options.CLIModel != "" {
		resolved := ResolveCliModel(ResolveCliModelOptions{
			CLIProvider:  options.CLIProvider,
			CLIModel:     options.CLIModel,
			ModelRuntime: options.ModelRuntime,
		})
		if resolved.Error != "" {
			return InitialModelResult{Error: resolved.Error, ThinkingLevel: DefaultThinkingLevel}
		}
		if resolved.Model != nil {
			return InitialModelResult{Model: resolved.Model, ThinkingLevel: DefaultThinkingLevel}
		}
	}

	if len(options.ScopedModels) > 0 && !options.IsContinuing {
		scoped := options.ScopedModels[0]
		perModel, hasPerModel := options.ModelThinkingLevels[scoped.Model.Provider+"/"+scoped.Model.ID]
		switch {
		case scoped.HasThinking:
			thinkingLevel = scoped.ThinkingLevel
		case hasPerModel:
			thinkingLevel = perModel
		case options.HasDefaultThinking:
			thinkingLevel = options.DefaultThinkingLevel
		default:
			thinkingLevel = DefaultThinkingLevel
		}
		return InitialModelResult{Model: scoped.Model, ThinkingLevel: thinkingLevel}
	}

	if options.DefaultProvider != "" && options.DefaultModelID != "" {
		found := options.ModelRuntime.GetModel(options.DefaultProvider, options.DefaultModelID)
		if found != nil && options.ModelRuntime.HasConfiguredAuth(found.Provider) {
			model = found
			if perModel, ok := options.ModelThinkingLevels[options.DefaultProvider+"/"+options.DefaultModelID]; ok {
				thinkingLevel = perModel
			} else if options.HasDefaultThinking {
				thinkingLevel = options.DefaultThinkingLevel
			}
			return InitialModelResult{Model: model, ThinkingLevel: thinkingLevel}
		}
	}

	availableModels := options.ModelRuntime.GetAvailableSnapshot()
	if len(availableModels) > 0 {
		for _, providerID := range DefaultProviderOrder {
			defaultID := DefaultModelPerProvider[providerID]
			for _, candidate := range availableModels {
				if candidate.Provider == providerID && candidate.ID == defaultID {
					return InitialModelResult{Model: candidate, ThinkingLevel: DefaultThinkingLevel}
				}
			}
		}
		return InitialModelResult{Model: availableModels[0], ThinkingLevel: DefaultThinkingLevel}
	}

	return InitialModelResult{ThinkingLevel: DefaultThinkingLevel}
}

// DefaultProviderOrder is the order providers are considered for defaults.
//
// D21: upstream iterates the object keys of defaultModelPerProvider (insertion
// order); Go maps have no order, so the declaration order is kept explicitly.
var DefaultProviderOrder = []ai.ProviderId{
	"amazon-bedrock", "ant-ling", "anthropic", "openai", "azure", "openai-codex",
	"radius", "nvidia", "deepseek", "google", "google-vertex", "github-copilot", "openrouter",
	"vercel-ai-gateway", "xai", "groq", "cerebras", "zai", "zai-coding-cn", "mistral", "minimax",
	"minimax-cn", "moonshotai", "moonshotai-cn", "huggingface", "fireworks", "together", "baseten",
	"opencode", "opencode-go", "kimi-coding", "meta", "cloudflare-workers-ai", "cloudflare-ai-gateway",
	"qwen-token-plan", "qwen-token-plan-cn", "qwen-token-plan-individual", "xiaomi",
	"xiaomi-token-plan-cn", "xiaomi-token-plan-ams", "xiaomi-token-plan-sgp",
}

// RestoreModelResult is the session model restore result.
type RestoreModelResult struct {
	Model           *ai.Model
	FallbackMessage string
}

// RestoreModelFromSession restores a session's model, falling back to available
// models (upstream restoreModelFromSession).
func RestoreModelFromSession(
	savedProvider, savedModelID string,
	currentModel *ai.Model,
	modelRuntime ModelRuntimeSource,
) RestoreModelResult {
	restoredModel := modelRuntime.GetModel(savedProvider, savedModelID)
	hasAuth := restoredModel != nil && modelRuntime.HasConfiguredAuth(restoredModel.Provider)
	if restoredModel != nil && hasAuth {
		return RestoreModelResult{Model: restoredModel}
	}

	reason := "model no longer exists"
	if restoredModel != nil {
		reason = "no auth configured"
	}
	if currentModel != nil {
		return RestoreModelResult{
			Model: currentModel,
			FallbackMessage: fmt.Sprintf("Could not restore model %s/%s (%s). Using %s/%s.",
				savedProvider, savedModelID, reason, currentModel.Provider, currentModel.ID),
		}
	}

	availableModels := modelRuntime.GetAvailableSnapshot()
	if len(availableModels) > 0 {
		var fallbackModel *ai.Model
		for _, providerID := range DefaultProviderOrder {
			defaultID := DefaultModelPerProvider[providerID]
			for _, candidate := range availableModels {
				if candidate.Provider == providerID && candidate.ID == defaultID {
					fallbackModel = candidate
					break
				}
			}
			if fallbackModel != nil {
				break
			}
		}
		if fallbackModel == nil {
			fallbackModel = availableModels[0]
		}
		return RestoreModelResult{
			Model: fallbackModel,
			FallbackMessage: fmt.Sprintf("Could not restore model %s/%s (%s). Using %s/%s.",
				savedProvider, savedModelID, reason, fallbackModel.Provider, fallbackModel.ID),
		}
	}
	return RestoreModelResult{}
}

// ModelRuntimeSource is the model-runtime surface the resolver needs (the Go
// subset of upstream ModelRuntime, satisfied by *ModelRuntime).
type ModelRuntimeSource interface {
	GetModels(providerID string) []*ai.Model
	GetModel(providerID, modelID string) *ai.Model
	GetAvailable(providerID string, ctx context.Context) ([]*ai.Model, error)
	GetAvailableSnapshot() []*ai.Model
	HasConfiguredAuth(providerID string) bool
}

// cache is the last full availability listing.

// ResolveModelScopeWithDiagnostics resolves patterns against the runtime.
func ResolveModelScopeWithDiagnostics(patterns []string, runtime ModelRuntimeSource, ctx context.Context) (ResolveModelScopeResult, error) {
	models, err := runtime.GetAvailable("", ctx)
	if err != nil {
		return ResolveModelScopeResult{}, err
	}
	return ResolveModelScopeFromModels(patterns, models), nil
}
