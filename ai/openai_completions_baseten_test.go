package ai

import (
	"testing"
)

// openai-completions.ts baseten branch: `mappedEffort = requestedEffort ?
// map?.[requestedEffort] : map?.off`, then `effort = mappedEffort === undefined
// ? requestedEffort : mappedEffort`, sent only when it is a string. At thinking
// off the requested effort is undefined, so an absent off mapping sends nothing
// rather than the literal "off".
func TestBuildOpenAICompletionsParamsBasetenOffEffort(t *testing.T) {
	build := func(levels ThinkingLevelMap) *OpenAICompletionsParams {
		model := &Model{
			ID: "baseten-model", API: APIOpenAICompletions, Provider: "baseten",
			Reasoning: true, ThinkingLevelMap: levels, ContextWindow: 100000, MaxTokens: 4096,
		}
		compat := GetOpenAICompletionsCompat(model)
		compat.ThinkingFormat = "baseten"
		compat.SupportsReasoningEffort = true
		params, err := BuildOpenAICompletionsParams(model, TranscriptContext{Messages: []Message{
			&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
		}}, &OpenAICompletionsOptions{}, &compat, CacheRetentionNone, map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		return params
	}

	if params := build(nil); params.ReasoningEffort != nil {
		t.Fatalf("reasoning_effort = %q at off with no off mapping, want none", *params.ReasoningEffort)
	}
	none := "none"
	if params := build(ThinkingLevelMap{ThinkOff: &none}); params.ReasoningEffort == nil || *params.ReasoningEffort != "none" {
		t.Fatalf("reasoning_effort = %v with an off mapping, want none", params.ReasoningEffort)
	}
}
