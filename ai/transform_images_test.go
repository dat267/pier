package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), api/transform-messages.ts
// `downgradeUnsupportedImages`, reached through `transformMessages`. Each case
// pairs a message set with a target model whose `input` decides whether images
// survive, and compares a projection of the transformed messages: role, content
// block types with their text (so a placeholder shows), and a tool result's id.
//
// Regenerate: node --experimental-strip-types testdata/transform-images/generate.mjs ../pi > testdata/transform-images/golden.json
func TestTransformImageDowngradeMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/transform-images/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string            `json:"name"`
		Model    Model             `json:"model"`
		Messages []json.RawMessage `json:"messages"`
		Want     string            `json:"want"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 8 {
		t.Fatalf("fixture has %d cases, expected the full corpus", len(cases))
	}
	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			var messages []Message
			for _, raw := range testCase.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			model := testCase.Model
			transformed := TransformMessages(messages, &model, nil)
			var want []map[string]any
			if err := json.Unmarshal([]byte(testCase.Want), &want); err != nil {
				t.Fatal(err)
			}
			gotJSON, err := json.Marshal(projectTransformed(transformed))
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("transformed = %s\nwant          %s", gotJSON, wantJSON)
			}
		})
	}
}

// projectTransformed matches the generator's projection: role, the tool result
// id, and each content block's type and text.
func projectTransformed(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		entry := map[string]any{"role": RoleOf(message)}
		if result, ok := message.(*ToolResultMessage); ok {
			entry["toolCallId"] = result.ToolCallID
		}
		var blocks []map[string]any
		for _, content := range projectContent(message) {
			block := map[string]any{"type": content.kind, "text": content.text}
			blocks = append(blocks, block)
		}
		entry["content"] = blocks
		out = append(out, entry)
	}
	return out
}

// projectContent lists a message's blocks as type and text, which is all the
// reference projection keeps.
func projectContent(message Message) []struct{ kind, text string } {
	var out []struct{ kind, text string }
	add := func(kind, text string) { out = append(out, struct{ kind, text string }{kind, text}) }
	switch m := message.(type) {
	case *UserMessage:
		if m.Content.Blocks == nil {
			add("string", m.Content.Text)
			return out
		}
		for _, block := range m.Content.Blocks {
			switch b := block.(type) {
			case TextContent:
				add("text", b.Text)
			case ImageContent:
				add("image", "")
			}
		}
	case *ToolResultMessage:
		for _, block := range m.Content {
			switch b := block.(type) {
			case TextContent:
				add("text", b.Text)
			case ImageContent:
				add("image", "")
			}
		}
	case *AssistantMessage:
		for _, block := range m.Content {
			switch b := block.(type) {
			case TextContent:
				add("text", b.Text)
			case ThinkingContent:
				add("thinking", b.Thinking)
			case ToolCall:
				add("toolCall", "")
			}
		}
	case *SystemMessage:
		add("string", m.Content.Text)
	}
	return out
}
