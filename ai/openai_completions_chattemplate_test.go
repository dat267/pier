package ai

import (
	"encoding/json"
	"testing"
)

// openai-completions.ts resolveChatTemplateKwargValue: a kwarg value is a
// literal, a `$var` object, or (the default branch) the thinking level mapped
// through the model's thinkingLevelMap. `map?.[level] === undefined` falls back
// to the requested level name, an explicit null mapping omits the key, and the
// off level has no name to fall back to, so an absent `off` mapping omits it.
func TestBuildChatTemplateValuesMatchesTheReference(t *testing.T) {
	raw := func(text string) json.RawMessage { return json.RawMessage(text) }
	mapped := func(level ThinkingLevel, value string) *string { return new(value) }
	levels := func(entries map[ThinkingLevel]*string) ThinkingLevelMap {
		if entries == nil {
			return nil
		}
		return ThinkingLevelMap(entries)
	}

	for _, testCase := range []struct {
		name    string
		values  map[string]json.RawMessage
		levels  ThinkingLevelMap
		effort  ThinkingLevel
		budget  *int
		want    map[string]any
		omitted []string
	}{
		{
			name:   "literal values pass through",
			values: map[string]json.RawMessage{"temperature": raw("0.7"), "flag": raw("true"), "text": raw(`"x"`)},
			want:   map[string]any{"temperature": 0.7, "flag": true, "text": "x"},
		},
		{
			name:   "thinking.enabled follows the level",
			values: map[string]json.RawMessage{"enable_thinking": raw(`{"$var":"thinking.enabled"}`)},
			effort: ThinkHigh,
			want:   map[string]any{"enable_thinking": true},
		},
		{
			name:   "thinking.enabled is false when off",
			values: map[string]json.RawMessage{"enable_thinking": raw(`{"$var":"thinking.enabled"}`)},
			effort: "",
			want:   map[string]any{"enable_thinking": false},
		},
		{
			name:   "thinking.budget uses the clamped budget",
			values: map[string]json.RawMessage{"thinking_budget": raw(`{"$var":"thinking.budget"}`)},
			effort: ThinkHigh,
			budget: new(4096),
			want:   map[string]any{"thinking_budget": 4096},
		},
		{
			name:    "thinking.budget without a budget omits the key",
			values:  map[string]json.RawMessage{"thinking_budget": raw(`{"$var":"thinking.budget"}`)},
			effort:  ThinkHigh,
			omitted: []string{"thinking_budget"},
		},
		{
			name:    "omitWhenOff drops the key when off",
			values:  map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level","omitWhenOff":true}`)},
			effort:  "",
			omitted: []string{"reasoning"},
		},
		{
			name:   "mapped level uses the mapping",
			values: map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			levels: levels(map[ThinkingLevel]*string{ThinkHigh: mapped(ThinkHigh, "HIGH")}),
			effort: ThinkHigh,
			want:   map[string]any{"reasoning": "HIGH"},
		},
		{
			name:   "unmapped level falls back to the level name",
			values: map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			effort: ThinkMedium,
			want:   map[string]any{"reasoning": "medium"},
		},
		{
			name:    "explicit null mapping omits the key",
			values:  map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			levels:  levels(map[ThinkingLevel]*string{ThinkHigh: nil}),
			effort:  ThinkHigh,
			omitted: []string{"reasoning"},
		},
		{
			name:   "off with a mapping uses it",
			values: map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			levels: levels(map[ThinkingLevel]*string{ThinkOff: mapped(ThinkOff, "none")}),
			effort: "",
			want:   map[string]any{"reasoning": "none"},
		},
		{
			// The reference's expression has no level name to fall back to when
			// off and the model has no off mapping, so the key is omitted.
			name:    "off without a mapping omits the key",
			values:  map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			effort:  "",
			omitted: []string{"reasoning"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			model := &Model{ID: "m", API: APIOpenAICompletions, ThinkingLevelMap: testCase.levels}
			options := &OpenAICompletionsOptions{ReasoningEffort: string(testCase.effort)}
			got := buildChatTemplateValues(model, options, testCase.values, testCase.budget)
			for key, want := range testCase.want {
				if value, ok := got[key]; !ok || value != want {
					t.Fatalf("%s = %v (present %v), want %v", key, value, ok, want)
				}
			}
			for _, key := range testCase.omitted {
				if value, ok := got[key]; ok {
					t.Fatalf("%s = %v, want the key omitted", key, value)
				}
			}
		})
	}
}
