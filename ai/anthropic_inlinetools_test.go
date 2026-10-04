package ai

import (
	"encoding/json"
	"testing"
)

func inlineTool(name, description string) Tool {
	return Tool{Name: name, Description: description, Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}
}

// TestAnthropicInlineToolChanges covers the inline-tools beta: the request-level
// tool list stays fixed (initial tool + placeholder), later tools are defined by
// value in tool_addition blocks, and a same-name redefinition needs no removal
// (b271b0a52).
func TestAnthropicInlineToolChanges(t *testing.T) {
	model := &Model{
		ID: "claude-opus-4-7", Provider: "anthropic", API: APIAnthropicMessages,
		ContextWindow: 200000, MaxTokens: 8192,
		Compat: &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{
			SupportsMidConvoSystemMessages: boolPtrForBedrock(true),
			SupportsMidConvoToolChanges:    boolPtrForBedrock(true),
		}},
	}
	context := TranscriptContext{Messages: []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "base"}, ToolsAdded: []Tool{inlineTool("read", "reads")}, Timestamp: 1},
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 2},
		&SystemMessage{Content: StringOrBlocks{Text: "more tools"}, ToolsAdded: []Tool{inlineTool("write", "writes")}, Timestamp: 3},
	}}
	params, err := BuildAnthropicParams(model, context, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The request-level list only holds the initial tool and the placeholder.
	if len(params.Tools) != 2 || params.Tools[0].Name != "read" || params.Tools[1].Name != "__pi_deferred_placeholder__" {
		t.Fatalf("tools = %+v", params.Tools)
	}
	// The beta feature is requested.
	if !containsBeta(params.Betas, InlineToolsBeta) {
		t.Fatalf("betas = %v", params.Betas)
	}
	// The later system message defines write inline.
	if !containsInlineDefinition(t, params.Messages, "write") {
		t.Fatal("no inline tool_definition for write")
	}

	// A same-name redefinition emits no tool_removal and one tool_addition with
	// the new definition.
	redefine := context
	redefine.Messages = []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "base"}, ToolsAdded: []Tool{inlineTool("read", "reads")}, Timestamp: 1},
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 2},
		&SystemMessage{Content: StringOrBlocks{Text: "better read"}, Timestamp: 3,
			ToolsRemoved: []ToolReference{{Name: "read"}}, ToolsAdded: []Tool{inlineTool("read", "reads better")}},
	}
	params, err = BuildAnthropicParams(model, redefine, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	removals, additions := 0, 0
	for _, message := range params.Messages {
		if message.Role != "system" {
			continue
		}
		var blocks []AnthropicContentBlock
		if err := json.Unmarshal(message.Content, &blocks); err != nil {
			t.Fatal(err)
		}
		for _, block := range blocks {
			switch block.Type {
			case "tool_removal":
				removals++
			case "tool_addition":
				additions++
				if block.Tool.Definition == nil || block.Tool.Definition.Description != "reads better" {
					t.Fatalf("redefinition = %+v", block.Tool)
				}
			}
		}
	}
	if removals != 0 || additions != 1 {
		t.Fatalf("removals = %d, additions = %d", removals, additions)
	}
}

func containsBeta(betas []string, want string) bool {
	for _, beta := range betas {
		if beta == want {
			return true
		}
	}
	return false
}

func containsInlineDefinition(t *testing.T, messages []AnthropicMessage, name string) bool {
	t.Helper()
	for _, message := range messages {
		if message.Role != "system" {
			continue
		}
		var blocks []AnthropicContentBlock
		if err := json.Unmarshal(message.Content, &blocks); err != nil {
			t.Fatal(err)
		}
		for _, block := range blocks {
			if block.Type != "tool_addition" || block.Tool == nil {
				continue
			}
			if block.Tool.Type != "tool_definition" || block.Tool.Definition == nil || block.Tool.Definition.Name != name {
				t.Fatalf("tool_addition = %+v", block.Tool)
			}
			return true
		}
	}
	return false
}
