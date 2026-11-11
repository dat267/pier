package ai

import (
	"fmt"
	"math"
	"time"
)

// Port of api/simple-options.ts.

const (
	contextSafetyTokens = 4096
	minMaxTokens        = 1
	// contextSafetyRatio is the input margin as a fraction of the counted
	// input, applied when it exceeds the fixed margin.
	contextSafetyRatio = 0.15
	// minOutputWithRatioMargin is the smallest output room that still justifies
	// the ratio margin; below it only the fixed margin is kept.
	minOutputWithRatioMargin = 32768

	// MinAnswerTokens are always left for the answer when a thinking budget
	// shares the response ceiling.
	MinAnswerTokens = 1024
)

// startSimpleStream performs the setup every simple stream entry point shares:
// default the options, create the event stream, and end it with the
// missing-key error when no API key is configured. ok is false when the caller
// must return stream unchanged.
func startSimpleStream(model *Model, options *SimpleStreamOptions) (stream *AssistantMessageEventStream, resolved *SimpleStreamOptions, ok bool) {
	if options == nil {
		options = &SimpleStreamOptions{}
	}
	stream = NewAssistantMessageEventStream()
	if options.APIKey != "" {
		return stream, options, true
	}
	go func() {
		message := fmt.Sprintf("No API key for provider: %s", model.Provider)
		msg := &AssistantMessage{
			API: model.API, Provider: model.Provider, Model: model.ID,
			Usage: Usage{Cost: UsageCost{}}, StopReason: StopError,
			ErrorMessage: &message, Timestamp: time.Now().UnixMilli(),
		}
		stream.Push(AssistantMessageEvent{Type: EventError, Reason: StopError, Error: msg})
		stream.End(&msg)
	}()
	return stream, options, false
}

// endStreamWithError ends stream with the stop-error message every provider
// builds for a request that never started.
func endStreamWithError(stream *AssistantMessageEventStream, model *Model, text string) {
	msg := &AssistantMessage{
		API: model.API, Provider: model.Provider, Model: model.ID,
		Usage: Usage{Cost: UsageCost{}}, StopReason: StopError,
		Timestamp: time.Now().UnixMilli(),
	}
	endStreamWithMessage(stream, msg, text)
}

// endStreamWithMessage ends stream with output carrying message as its error.
func endStreamWithMessage(stream *AssistantMessageEventStream, output *AssistantMessage, message string) {
	output.ErrorMessage = &message
	stream.Push(AssistantMessageEvent{Type: EventError, Reason: output.StopReason, Error: output})
	stream.End(&output)
}

// ClampMaxTokensToContext clamps a maxTokens value to what fits in the
// context window alongside the current transcript.
func ClampMaxTokensToContext(model *Model, context TranscriptContext, maxTokens int) int {
	if model.ContextWindow <= 0 {
		return max(minMaxTokens, maxTokens)
	}
	// The arithmetic stays in float64 like upstream's Numbers: D313 saturates
	// an oversized usage estimate at the int bound, and integer subtraction
	// there would wrap a huge negative room back into positive answer room.
	input := float64(EstimateContextTokens(context.Messages).Tokens)
	// OpenRouter counts the input with its own tokenizer and rejects
	// input + max_tokens above the window before routing, counting prose up to
	// 12% above the provider's count (#10497), so keep a 15% input margin.
	margin := math.Max(float64(contextSafetyTokens), math.Ceil(input*contextSafetyRatio))
	room := float64(model.ContextWindow) - input - margin
	if room < float64(minOutputWithRatioMargin) {
		// The ratio margin only applies while it leaves room to answer: a
		// nearly full large window, or a small one, keeps the fixed margin and
		// caps output at that size.
		room = math.Min(float64(minOutputWithRatioMargin), float64(model.ContextWindow)-input-float64(contextSafetyTokens))
	}
	return jsNumberToInt(math.Min(float64(maxTokens), math.Max(float64(minMaxTokens), room)))
}

// withSimpleMaxTokens ports buildBaseOptions' output ceiling without changing
// raw stream options: simple calls default to the model cap and reserve room
// for the current transcript. Other base options are passed through unchanged.
func withSimpleMaxTokens(model *Model, context TranscriptContext, options StreamOptions) StreamOptions {
	maxTokens := int(model.MaxTokens)
	if options.MaxTokens != nil {
		maxTokens = *options.MaxTokens
	}
	clamped := ClampMaxTokensToContext(model, context, maxTokens)
	options.MaxTokens = &clamped
	return options
}

// DefaultThinkingBudgets are the per-level thinking token budgets.
var DefaultThinkingBudgets = ThinkingBudgets{
	Minimal: new(1024),
	Low:     new(2048),
	Medium:  new(8192),
	High:    new(16384),
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
