package ai

import (
	"encoding/json"
	"testing"
)

// Reference: pi f9a26365 api/pi-messages.ts toolcall_end does
// `Object.assign(partial.content[i], event.toolCall)`, which copies only the
// members the wire object carries. A toolCall that omits arguments (or is
// null) keeps the arguments accumulated from toolcall_delta.
func TestPiMessagesToolCallEndKeepsStreamedArguments(t *testing.T) {
	cases := map[string]json.RawMessage{
		"arguments omitted": json.RawMessage(`{"type":"toolCall","id":"call-1","name":"read"}`),
		"toolCall null":     json.RawMessage(`null`),
	}
	for name, endToolCall := range cases {
		t.Run(name, func(t *testing.T) {
			converter := newPiMessagesConverter(&Model{API: APIPiMessages, Provider: "p", ID: "m"})
			events := []PiMessagesEvent{
				{Type: "toolcall_start", ContentIndex: 0, ID: "call-1", ToolName: "read"},
				{Type: "toolcall_delta", ContentIndex: 0, Delta: `{"path":"a"}`},
				{Type: "toolcall_end", ContentIndex: 0, ToolCall: endToolCall},
			}
			for index := range events {
				converter.convert(&events[index])
			}
			if len(converter.partial.Content) != 1 {
				t.Fatalf("content = %+v", converter.partial.Content)
			}
			call, ok := converter.partial.Content[0].(ToolCall)
			if !ok {
				t.Fatalf("content = %T", converter.partial.Content[0])
			}
			if string(call.Arguments) != `{"path":"a"}` {
				t.Fatalf("arguments = %s; want the streamed object", call.Arguments)
			}
			if call.ID != "call-1" || call.Name != "read" {
				t.Fatalf("identity = %q/%q", call.ID, call.Name)
			}
		})
	}
}
