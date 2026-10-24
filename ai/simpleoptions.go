package ai

// Port of api/simple-options.ts.

const (
	contextSafetyTokens = 4096
	minMaxTokens        = 1

	// MinAnswerTokens are always left for the answer when a thinking budget
	// shares the response ceiling.
	MinAnswerTokens = 1024
)

// ClampMaxTokensToContext clamps a maxTokens value to what fits in the
// context window alongside the current transcript.
func ClampMaxTokensToContext(model *Model, context TranscriptContext, maxTokens int) int {
	if model.ContextWindow <= 0 {
		return max(minMaxTokens, maxTokens)
	}
	available := int(model.ContextWindow) - EstimateContextTokens(context.Messages).Tokens - contextSafetyTokens
	return min(maxTokens, max(minMaxTokens, available))
}

// DefaultThinkingBudgets are the per-level thinking token budgets.
var DefaultThinkingBudgets = ThinkingBudgets{
	Minimal: intPtr(1024),
	Low:     intPtr(2048),
	Medium:  intPtr(8192),
	High:    intPtr(16384),
}

// samplingThinkingLevel maps an absent reasoning effort to off, the
// `options?.reasoningEffort ?? "off"` the sampling resolver expects.
func samplingThinkingLevel(effort ThinkingLevel) ModelThinkingLevel {
	if effort == "" {
		return ThinkOff
	}
	return effort
}

// ResolveSamplingParams merges the model defaults, the parameters selected by
// the effective thinking level, and the request parameters, in that order (the
// port of resolveSamplingParams in api/simple-options.ts).
func ResolveSamplingParams(model *Model, thinkingLevel ModelThinkingLevel, requestParams SamplingParams) SamplingParams {
	params, _ := ResolveSamplingParamsWithOrder(model, thinkingLevel, requestParams, nil)
	return params
}

// ResolveSamplingParamsWithOrder resolves values and preserves first-assignment
// order across model defaults, thinking-level overrides, and request values.
func ResolveSamplingParamsWithOrder(model *Model, thinkingLevel ModelThinkingLevel, requestParams SamplingParams, requestOrder []string) (SamplingParams, []string) {
	effective := ClampThinkingLevel(model, thinkingLevel)
	levelParams := model.SamplingParamsByThinkingLevel[effective]
	if model.SamplingParams == nil && levelParams == nil && len(requestParams) == 0 {
		return nil, nil
	}
	merged := SamplingParams{}
	order := make([]string, 0, len(model.SamplingParams)+len(levelParams)+len(requestParams))
	merge := func(params SamplingParams, preferred []string) {
		seen := make(map[string]bool, len(params))
		for _, key := range preferred {
			if _, ok := params[key]; ok && !seen[key] {
				if _, exists := merged[key]; !exists {
					order = append(order, key)
				}
				merged[key] = params[key]
				seen[key] = true
			}
		}
		remaining := make([]string, 0, len(params)-len(seen))
		for key := range params {
			if !seen[key] {
				remaining = append(remaining, key)
			}
		}
		sortStrings(remaining)
		for _, key := range remaining {
			if _, exists := merged[key]; !exists {
				order = append(order, key)
			}
			merged[key] = params[key]
		}
	}
	merge(model.SamplingParams, model.SamplingParamsOrder)
	merge(levelParams, model.SamplingParamsByThinkingLevelOrder[effective])
	merge(requestParams, requestOrder)
	return merged, order
}

func orderedSamplingKeys(params SamplingParams, preferred []string) []string {
	order := make([]string, 0, len(params))
	seen := make(map[string]bool, len(params))
	for _, key := range preferred {
		if _, ok := params[key]; ok && !seen[key] {
			order = append(order, key)
			seen[key] = true
		}
	}
	remaining := make([]string, 0, len(params)-len(seen))
	for key := range params {
		if !seen[key] {
			remaining = append(remaining, key)
		}
	}
	sortStrings(remaining)
	return append(order, remaining...)
}

func intPtr(n int) *int { return &n }

// ClampReasoning maps xhigh/max down to high for budget-based thinking.
func ClampReasoning(effort ThinkingLevel) ThinkingLevel {
	if effort == ThinkXHigh || effort == ThinkMax {
		return ThinkHigh
	}
	return effort
}

// ThinkingBudgetForLevel resolves the thinking budget for a level.
func ThinkingBudgetForLevel(reasoningLevel ThinkingLevel, customBudgets *ThinkingBudgets) int {
	budgetFor := func(level ThinkingLevel) *int {
		if customBudgets == nil {
			switch level {
			case ThinkMinimal:
				return DefaultThinkingBudgets.Minimal
			case ThinkLow:
				return DefaultThinkingBudgets.Low
			case ThinkMedium:
				return DefaultThinkingBudgets.Medium
			case ThinkHigh:
				return DefaultThinkingBudgets.High
			}
			return nil
		}
		switch level {
		case ThinkMinimal:
			if customBudgets.Minimal != nil {
				return customBudgets.Minimal
			}
			return DefaultThinkingBudgets.Minimal
		case ThinkLow:
			if customBudgets.Low != nil {
				return customBudgets.Low
			}
			return DefaultThinkingBudgets.Low
		case ThinkMedium:
			if customBudgets.Medium != nil {
				return customBudgets.Medium
			}
			return DefaultThinkingBudgets.Medium
		case ThinkHigh:
			if customBudgets.High != nil {
				return customBudgets.High
			}
			return DefaultThinkingBudgets.High
		}
		return nil
	}
	level := ClampReasoning(reasoningLevel)
	if b := budgetFor(level); b != nil {
		return *b
	}
	if b := budgetFor(ThinkHigh); b != nil {
		return *b
	}
	return 1024
}

// ClampThinkingBudgetToAnswerRoom caps a thinking budget so at least
// MinAnswerTokens remain under a shared response ceiling.
func ClampThinkingBudgetToAnswerRoom(thinkingBudget, ceiling int) int {
	return min(thinkingBudget, max(0, ceiling-MinAnswerTokens))
}

// AdjustMaxTokensForThinking fits a thinking budget under a maxTokens cap.
// A nil baseMaxTokens means no explicit caller cap: use the model cap and
// fit thinking inside it.
func AdjustMaxTokensForThinking(baseMaxTokens *int, modelMaxTokens int, reasoningLevel ThinkingLevel, customBudgets *ThinkingBudgets) (maxTokens, thinkingBudget int) {
	thinkingBudget = ThinkingBudgetForLevel(reasoningLevel, customBudgets)
	if baseMaxTokens == nil {
		maxTokens = modelMaxTokens
	} else {
		maxTokens = min(*baseMaxTokens+thinkingBudget, modelMaxTokens)
	}
	if maxTokens <= thinkingBudget {
		thinkingBudget = ClampThinkingBudgetToAnswerRoom(thinkingBudget, maxTokens)
	}
	return maxTokens, thinkingBudget
}
