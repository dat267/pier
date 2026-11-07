package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) packages/ai/src/api/mistral-conversations.ts
// buildChatPayload builds `messages: toChatMessages(...)` unconditionally
// (array, never null), and toChatMessages returns an empty array when no
// message survives the conversion (an assistant whose only content is an
// empty-text block is one such input). A423: pier marshalled the nil slice,
// so the wire writes `"messages": null` where the reference writes `[]`.
func TestMistralMessagesIsAnArrayEvenWhenEmpty(t *testing.T) {
	messages := []Message{
		&AssistantMessage{Content: ContentList{TextContent{Text: "  "}}},
	}
	got := toMistralChatMessages(messages, false)
	if len(got) != 0 {
		t.Fatalf("messages = %#v, want none", got)
	}
	payload := &mistralChatPayload{
		Model: "codestral-2508", Stream: true, Messages: got,
	}
	encoded, err := mistralWirePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(encoded, &probe); err != nil {
		t.Fatal(err)
	}
	if _, ok := probe["messages"]; !ok {
		t.Fatalf("messages key missing: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"messages":[]`) {
		t.Fatalf("wire = %s; want an empty messages array", encoded)
	}
	if !strings.Contains(string(encoded), `"messages":null`) {
		t.Logf("the empty form is pinned: %s", encoded)
	}
}
