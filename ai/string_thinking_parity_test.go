package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStringThinkingNullSendsTheLevelName covers upstream's
// `thinking = map?.[effort] ?? effort` (openai-completions.ts 949-950): a null
// mapping must send the level name, not the "none" off-marker.
func TestStringThinkingNullSendsTheLevelName(t *testing.T) {
	explicitNull := (*string)(nil)
	model := &Model{
		ID: "m", Name: "m", API: APIOpenAICompletions, Provider: "custom",
		BaseURL: "https://invalid.test/v1", Reasoning: true, Input: []string{"text"},
		ContextWindow: 1000, MaxTokens: 100,
		ThinkingLevelMap: ThinkingLevelMap{ThinkOff: new("none"), ThinkMedium: explicitNull},
		Compat: &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{
			ThinkingFormat: new("string-thinking"),
		}},
	}
	params, err := BuildOpenAICompletionsParams(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{
		ReasoningEffort: ThinkMedium,
	}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Thinking *string `json:"thinking"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Thinking == nil || *decoded.Thinking != "medium" {
		t.Fatalf("thinking = %v, want medium; body %s", decoded.Thinking, encoded)
	}
	// The off branch keeps the "none" fallback for a missing off mapping.
	off := &Model{ID: "m", API: APIOpenAICompletions, Provider: "custom",
		BaseURL: "https://invalid.test/v1", Reasoning: true,
		ThinkingLevelMap: ThinkingLevelMap{}, Compat: model.Compat}
	params, err = BuildOpenAICompletionsParams(off, NormalizeContext(Context{}), nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"thinking":"none"`; !strings.Contains(string(encoded), want) {
		t.Fatalf("off body %s, want %s", encoded, want)
	}
}
