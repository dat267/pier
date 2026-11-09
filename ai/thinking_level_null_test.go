package ai

import (
	"encoding/json"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) api/openai-completions.ts buildParams. The
// thinking-format branches disagree about an explicit `thinkingLevelMap[level]
// === null`:
//
//   - zai (872-875) and baseten (906-911) test `mappedEffort === undefined`
//     before a `typeof effort === "string"` guard, so null suppresses the field.
//   - qwen (881-884), deepseek (919-921), openrouter (926-928), together (945),
//     string-thinking (949-950) and the plain OpenAI branch (956) use
//     `map?.[level] ?? level`, and JavaScript's ?? accepts null as nullish, so
//     null sends the requested level name.
//
// A 535-model differential over every openai-completions entry in the builtin
// catalog (identical model data on both sides, request: one user message,
// maxTokens 1000, temperature 0.3, reasoningEffort "medium") found 89 payload
// mismatches, all from the nullish branches.
func TestThinkingLevelNullFallsBackToTheRequestedLevel(t *testing.T) {
	explicitNull := (*string)(nil)
	high := "high"
	mapped := ThinkingLevelMap{
		ThinkOff:     new("none"),
		ThinkMinimal: explicitNull,
		ThinkLow:     new("low"),
		ThinkMedium:  explicitNull,
		ThinkHigh:    &high,
	}
	reasoning := func(format string) *Model {
		return &Model{
			ID: "m", Name: "m", API: APIOpenAICompletions, Provider: "custom",
			BaseURL: "https://invalid.test/v1", Reasoning: true, Input: []string{"text"},
			ContextWindow: 1000, MaxTokens: 100,
			ThinkingLevelMap: mapped,
			Compat: &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{
				ThinkingFormat:          new(format),
				SupportsReasoningEffort: new(true),
			}},
		}
	}
	context := NormalizeContext(Context{})
	body := func(t *testing.T, format string, effort ThinkingLevel) map[string]json.RawMessage {
		t.Helper()
		params, err := BuildOpenAICompletionsParams(reasoning(format), context, &OpenAICompletionsOptions{
			ReasoningEffort: effort,
		}, nil, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := MarshalJSON(params)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}

	// The nullish branches send the level name for a null mapping.
	// openrouter nests it in `reasoning: {effort}`.
	for _, format := range []string{"openai", "deepseek", "together", "qwen"} {
		got := body(t, format, ThinkMedium)
		if string(got["reasoning_effort"]) != `"medium"` {
			t.Errorf("%s: reasoning_effort = %s, want \"medium\"", format, got["reasoning_effort"])
		}
	}
	if got := body(t, "openrouter", ThinkMedium); string(got["reasoning"]) != `{"effort":"medium"}` {
		t.Errorf("openrouter: reasoning = %s, want {\"effort\":\"medium\"}", got["reasoning"])
	}
	// A mapped level still wins over the level name.
	if got := body(t, "openai", ThinkHigh); string(got["reasoning_effort"]) != `"high"` {
		t.Errorf("openai high: reasoning_effort = %s, want \"high\"", got["reasoning_effort"])
	}
	// The undefined-only branches keep null as "unsupported".
	if got := body(t, "zai", ThinkMedium); got["reasoning_effort"] != nil {
		t.Errorf("zai: reasoning_effort = %s, want absent", got["reasoning_effort"])
	}
	if got := body(t, "baseten", ThinkMedium); got["reasoning_effort"] != nil {
		t.Errorf("baseten: reasoning_effort = %s, want absent", got["reasoning_effort"])
	}
}
