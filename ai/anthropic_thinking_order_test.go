package ai

import (
	"bytes"
	"testing"
)

// pi v1.4.0 (4a080252), packages/ai/src/api/anthropic-messages.ts buildParams:
// budget-based thinking is built as { type, budget_tokens, display }. The
// struct field order must preserve those bytes.
func TestAnthropicBudgetThinkingKeyOrder(t *testing.T) {
	model := &Model{
		Provider: "anthropic", ID: "claude-sonnet-4", API: "anthropic-messages",
		BaseURL: "https://api.anthropic.com", Reasoning: true,
	}
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}
	budget := 2048
	params, err := BuildAnthropicParams(model, context, false, &AnthropicOptions{
		ThinkingEnabled:      new(true),
		ThinkingBudgetTokens: &budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte(`"thinking":{"type":"enabled","budget_tokens":2048,"display":"summarized"}`); !bytes.Contains(encoded, want) {
		t.Fatalf("thinking bytes differ\n got: %s\nwant substring: %s", encoded, want)
	}

	adaptive := *model
	force := true
	adaptive.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{ForceAdaptiveThinking: &force}}
	params, err = BuildAnthropicParams(&adaptive, context, false, &AnthropicOptions{ThinkingEnabled: new(true)})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"thinking":{"type":"adaptive","display":"summarized"}`)) {
		t.Fatalf("adaptive thinking bytes differ: %s", encoded)
	}
}
