package ai

import "testing"

// Reference: pi v1.4.0 api/transform-messages.ts checks signature truthiness,
// so an empty signature does not preserve an empty unsigned thinking block.
func TestTransformThinkingUsesSignatureTruthiness(t *testing.T) {
	empty, signature := "", "opaque"
	model := &Model{ID: "m", Provider: "p", API: APIAnthropicMessages, Input: []string{"text"}}
	for _, tc := range []struct {
		name, text     string
		signature      *string
		redacted, want bool
	}{
		{name: "empty signature", signature: &empty},
		{name: "empty signature whitespace", text: "\u00a0", signature: &empty},
		{name: "unsigned whitespace", text: " ", signature: nil},
		{name: "signed empty", signature: &signature, want: true},
		{name: "redacted empty", signature: &empty, redacted: true, want: true},
		{name: "readable thinking", text: "reasoning", signature: &empty, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := &AssistantMessage{Provider: "p", Model: "m", API: APIAnthropicMessages, StopReason: StopStop, Content: ContentList{ThinkingContent{Thinking: tc.text, ThinkingSignature: tc.signature, Redacted: tc.redacted}}}
			out := TransformMessages([]Message{msg}, model, nil)
			if len(out) != 1 {
				t.Fatalf("messages=%d", len(out))
			}
			if got := len(out[0].(*AssistantMessage).Content) > 0; got != tc.want {
				t.Fatalf("thinking retained=%v want=%v", got, tc.want)
			}
		})
	}
}

// Reference: transform-messages.ts strips cross-model tool thought signatures
// only when truthy, preserving an explicitly empty signature in the transcript.
func TestTransformToolSignatureUsesTruthiness(t *testing.T) {
	empty := ""
	model := &Model{ID: "new", Provider: "p", API: APIAnthropicMessages, Input: []string{"text"}}
	msg := &AssistantMessage{Provider: "p", Model: "old", API: APIAnthropicMessages, StopReason: StopStop, Content: ContentList{ToolCall{ID: "call", Name: "read", ThoughtSignature: &empty}}}
	out := TransformMessages([]Message{msg}, model, nil)
	call := out[0].(*AssistantMessage).Content[0].(ToolCall)
	if call.ThoughtSignature == nil || *call.ThoughtSignature != "" {
		t.Fatalf("empty signature was dropped: %+v", call)
	}
}
