package ai

import (
	"strings"
	"testing"
)

func assertWireKeys(t *testing.T, encoded []byte, want ...string) {
	t.Helper()
	got := orderedKeys(encoded)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keys = %v\nwant %v\nbody %s", got, want, encoded)
	}
}

// Reference: pi v1.4.0 api/openai-completions.ts buildParams. The params
// object literal fixes the insertion order; later assignments replace a member
// in place or append one. Request payloads are byte-compared, so sorted keys
// are a divergence, not a detail.
func TestOpenAICompletionsParamsEmitJavaScriptKeyOrder(t *testing.T) {
	params := &OpenAICompletionsParams{
		Model: "m", Messages: []OpenAIMessage{{Role: "user", Content: []byte(`"hi"`)}}, Stream: true,
		StreamOptions:       &OpenAIStreamOptions{IncludeUsage: true},
		Store:               new(false),
		MaxCompletionTokens: new(int64(100)),
		Temperature:         new(0.3),
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	assertWireKeys(t, encoded, "model", "messages", "stream", "stream_options", "store", "max_completion_tokens", "temperature")
}

// Reference: buildParams assigns tool_stream inside the tools branch and
// tool_choice afterwards, so tool_stream precedes tool_choice.
func TestOpenAICompletionsToolFieldsKeepUpstreamOrder(t *testing.T) {
	params := &OpenAICompletionsParams{
		Model: "m", Messages: []OpenAIMessage{{Role: "user", Content: []byte(`"hi"`)}}, Stream: true,
		Tools:      []OpenAITool{{Type: "function", Function: &OpenAIFunctionDef{Name: "f"}}},
		ToolStream: new(true),
		ToolChoice: []byte(`"auto"`),
		Priority:   new(float64(1)),
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	assertWireKeys(t, encoded, "model", "messages", "stream", "tools", "tool_stream", "tool_choice", "priority")
}

// Reference: buildParams' zai and together branches assign thinking/reasoning
// before reasoning_effort; only one format applies per request.
func TestOpenAICompletionsThinkingFieldsKeepUpstreamOrder(t *testing.T) {
	zai := &OpenAICompletionsParams{
		Model: "m", Messages: []OpenAIMessage{{Role: "user", Content: []byte(`"hi"`)}}, Stream: true,
		Thinking:        &OpenAIThinkingParam{Type: "enabled"},
		ReasoningEffort: new("high"),
	}
	encoded, err := MarshalJSON(zai)
	if err != nil {
		t.Fatal(err)
	}
	assertWireKeys(t, encoded, "model", "messages", "stream", "thinking", "reasoning_effort")

	together := &OpenAICompletionsParams{
		Model: "m", Messages: []OpenAIMessage{{Role: "user", Content: []byte(`"hi"`)}}, Stream: true,
		ReasoningObj:    &OpenAIReasoningParam{Enabled: new(true)},
		ReasoningEffort: new("high"),
	}
	encoded, err = MarshalJSON(together)
	if err != nil {
		t.Fatal(err)
	}
	assertWireKeys(t, encoded, "model", "messages", "stream", "reasoning", "reasoning_effort")
}

// Reference: pi v1.4.0 api/openai-responses.ts buildParams. The team's params
// object literal puts input before stream and the cache options before store.
func TestOpenAIResponsesParamsEmitJavaScriptKeyOrder(t *testing.T) {
	params := &OpenAIResponsesParams{
		Model:              "m",
		Input:              []byte(`[]`),
		Stream:             true,
		PromptCacheKey:     new("sess"),
		PromptCacheOptions: &OpenAIPromptCacheOptions{Mode: "explicit"},
		Store:              new(false),
		MaxOutputTokens:    new(int64(16)),
		Temperature:        new(0.3),
		ServiceTier:        new("priority"),
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	assertWireKeys(t, encoded, "model", "input", "stream", "prompt_cache_key", "prompt_cache_options", "store", "max_output_tokens", "temperature", "service_tier")
}
