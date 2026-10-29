package ai

import "testing"

// pi v1.4.0 (4a080252), packages/ai/src/api/openai-completions.ts buildParams
// openrouter branch: with no requested effort, `model.thinkingLevelMap?.off ?? "none"`
// is sent. A missing off mapping therefore yields "none", not the literal "off".
func TestOpenRouterOffReasoningEffortDefaultsToNone(t *testing.T) {
	newParams := func(model *Model, options *OpenAICompletionsOptions, retention CacheRetention) *OpenAICompletionsParams {
		t.Helper()
		compat := DetectOpenAICompletionsCompat(model)
		params, err := BuildOpenAICompletionsParams(model, TranscriptContext{Messages: []Message{
			&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
		}}, options, &compat, retention, nil)
		if err != nil {
			t.Fatalf("BuildOpenAICompletionsParams: %v", err)
		}
		return params
	}
	model := &Model{
		Provider: "openrouter", ID: "reasoning-model", API: APIOpenAICompletions,
		BaseURL: "https://openrouter.ai/api/v1", Reasoning: true,
	}
	params := newParams(model, &OpenAICompletionsOptions{}, CacheRetentionShort)
	if params.ReasoningObj == nil || params.ReasoningObj.Effort == nil || *params.ReasoningObj.Effort != "none" {
		t.Fatalf("missing off mapping: reasoning = %+v; want effort \"none\"", params.ReasoningObj)
	}

	mapped := *model
	mapped.ThinkingLevelMap = map[ThinkingLevel]*string{ThinkOff: new("low")}
	params = newParams(&mapped, &OpenAICompletionsOptions{}, CacheRetentionShort)
	if params.ReasoningObj == nil || params.ReasoningObj.Effort == nil || *params.ReasoningObj.Effort != "low" {
		t.Fatalf("mapped off: reasoning = %+v; want effort \"low\"", params.ReasoningObj)
	}

	disabled := *model
	disabled.ThinkingLevelMap = map[ThinkingLevel]*string{ThinkOff: nil}
	params = newParams(&disabled, &OpenAICompletionsOptions{}, CacheRetentionShort)
	if params.ReasoningObj != nil {
		t.Fatalf("explicit null off must omit reasoning, got %+v", params.ReasoningObj)
	}

	requested := *model
	requested.ThinkingLevelMap = map[ThinkingLevel]*string{ThinkHigh: new("xhigh")}
	params = newParams(&requested, &OpenAICompletionsOptions{ReasoningEffort: ThinkHigh}, CacheRetentionShort)
	if params.ReasoningObj == nil || params.ReasoningObj.Effort == nil || *params.ReasoningObj.Effort != "xhigh" {
		t.Fatalf("requested high: reasoning = %+v; want effort \"xhigh\"", params.ReasoningObj)
	}
}
