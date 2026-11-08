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

// Reference: pi v1.5.0 api/transform-messages.ts replaceImagesWithPlaceholder
// maps block content to a new array, so an empty block list stays an empty
// array for text-only models; openai-completions then skips the message
// instead of sending a "" string, and its params start as [].
func TestTransformTextOnlyKeepsEmptyBlockContent(t *testing.T) {
	model := &Model{ID: "m", Provider: "p", API: APIOpenAICompletions, Input: []string{"text"}}
	out := TransformMessages([]Message{&UserMessage{Content: StringOrBlocks{Blocks: ContentList{}}, Timestamp: 1}}, model, nil)
	if user := out[0].(*UserMessage); user.Content.String() {
		t.Fatalf("content = %+v, want an empty block list", user.Content)
	}
	messages := ConvertOpenAICompletionsMessages(model, NormalizeContext(Context{Messages: out}), GetOpenAICompletionsCompat(model), nil)
	body, err := MarshalJSON(messages)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "[]" {
		t.Fatalf("messages = %s, want []", body)
	}
}
