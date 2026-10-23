package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Tests for utils/transcript.ts semantics beyond the system-message replay
// covered in message_json_test.go.

func testTool(name string) Tool {
	return Tool{Name: name, Description: name + " tool", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func TestGetCurrentToolsRemovalAndReaddition(t *testing.T) {
	messages := []Message{
		&SystemMessage{
			Content:    StringOrBlocks{Text: "base"},
			ToolsAdded: []Tool{testTool("base_tool")},
			Timestamp:  0,
		},
		&UserMessage{Content: StringOrBlocks{Text: "u"}, Timestamp: 1},
		&SystemMessage{
			Content:      StringOrBlocks{Text: "updated"},
			ToolsRemoved: []ToolReference{{Name: "base_tool"}},
			ToolsAdded:   []Tool{testTool("late_tool")},
			Timestamp:    2,
		},
	}
	tools := GetCurrentTools(messages)
	if len(tools) != 1 || tools[0].Name != "late_tool" {
		t.Fatalf("tools = %v; want [late_tool]", tools)
	}
}

// TranscriptToolSupport modes (upstream v1.4.0 caa6967b6): "none" keeps the
// current tools at the top, "changes" declares every tool in place, and
// "additions" folds the prefix up to the last removal or redeclaration so later
// loads stay anchored.
func TestResolveTranscriptToolsSupportModes(t *testing.T) {
	baseTool, lateTool := testTool("base_tool"), testTool("late_tool")
	additionContext := []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "base prompt"}, ToolsAdded: []Tool{baseTool}, Timestamp: 0},
		&UserMessage{Content: StringOrBlocks{Text: "before"}, Timestamp: 1},
		&SystemMessage{Content: StringOrBlocks{Text: "updated guidance"}, ToolsAdded: []Tool{lateTool}, Timestamp: 2},
	}
	removalContext := []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "base prompt"}, ToolsAdded: []Tool{baseTool}, Timestamp: 0},
		&UserMessage{Content: StringOrBlocks{Text: "before"}, Timestamp: 1},
		&SystemMessage{
			Content:      StringOrBlocks{Text: "updated guidance"},
			ToolsRemoved: []ToolReference{{Name: "base_tool"}},
			ToolsAdded:   []Tool{lateTool},
			Timestamp:    2,
		},
	}

	// Additions without a non-additive change anchor after the leading message.
	got := ResolveTranscriptTools(additionContext, TranscriptToolSupportAdditions)
	if got.FoldedSystemMessages != 1 {
		t.Fatalf("folded = %d; want the leading system message only", got.FoldedSystemMessages)
	}
	if names := toolNames(got.RequestTools); !reflect.DeepEqual(names, []string{"base_tool"}) {
		t.Fatalf("requestTools = %v; want [base_tool]", names)
	}

	// A removal folds everything up to and including that message, so later
	// loads stay anchored instead of resending the whole list.
	got = ResolveTranscriptTools(removalContext, TranscriptToolSupportAdditions)
	if got.FoldedSystemMessages != 2 {
		t.Fatalf("folded = %d; want both system messages", got.FoldedSystemMessages)
	}
	if names := toolNames(got.RequestTools); !reflect.DeepEqual(names, []string{"late_tool"}) {
		t.Fatalf("requestTools = %v; want [late_tool]", names)
	}

	// A redeclaration counts even after the tool was removed.
	redeclared := []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "base"}, ToolsAdded: []Tool{baseTool}, Timestamp: 0},
		&SystemMessage{
			Content:      StringOrBlocks{Text: "remove"},
			ToolsRemoved: []ToolReference{{Name: "base_tool"}},
			Timestamp:    1,
		},
		&UserMessage{Content: StringOrBlocks{Text: "u"}, Timestamp: 2},
		&SystemMessage{Content: StringOrBlocks{Text: "again"}, ToolsAdded: []Tool{baseTool}, Timestamp: 3},
	}
	got = ResolveTranscriptTools(redeclared, TranscriptToolSupportAdditions)
	if got.FoldedSystemMessages != 3 {
		t.Fatalf("folded = %d; want all three system messages", got.FoldedSystemMessages)
	}

	// "changes": nothing at the top; every message declares in place.
	got = ResolveTranscriptTools(removalContext, TranscriptToolSupportChanges)
	if got.FoldedSystemMessages != 0 || len(got.RequestTools) != 0 {
		t.Fatalf("changes = %+v; want an empty top-level list", got)
	}

	// "none": the top-level list is the current set and every system message is
	// folded into it.
	got = ResolveTranscriptTools(removalContext, TranscriptToolSupportNone)
	if got.FoldedSystemMessages != 2 {
		t.Fatalf("folded = %d; want both system messages", got.FoldedSystemMessages)
	}
	if names := toolNames(got.RequestTools); !reflect.DeepEqual(names, []string{"late_tool"}) {
		t.Fatalf("requestTools = %v; want [late_tool]", names)
	}
}

func toolNames(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestCollapseAndResolveTranscript(t *testing.T) {
	base := &SystemMessage{Content: StringOrBlocks{Text: "base"}, Timestamp: 0}
	base.SetSection("rules", strptr("old"))
	late := &SystemMessage{Content: StringOrBlocks{Text: "extra"}, Timestamp: 2}
	late.SetSection("rules", strptr("new"))
	ctx := TranscriptContext{Messages: []Message{
		base, &UserMessage{Content: StringOrBlocks{Text: "u"}, Timestamp: 1}, late,
	}}

	kept := ResolveTranscript(ctx, true)
	if len(kept.Messages) != 3 {
		t.Fatalf("mid-convo kept %d messages; want 3", len(kept.Messages))
	}
	collapsed := ResolveTranscript(ctx, false)
	if len(collapsed.Messages) != 2 {
		t.Fatalf("collapsed %d messages; want 2", len(collapsed.Messages))
	}
	head := collapsed.Messages[0].(*SystemMessage)
	if head.Content.Text != "base\n\nextra" {
		t.Fatalf("head content = %q", head.Content.Text)
	}
	if v := head.Sections["rules"]; v == nil || *v != "new" {
		t.Fatalf("rules = %v", head.Sections["rules"])
	}
}

func TestToolStateChangesAndRedefinitions(t *testing.T) {
	prev := []Tool{testTool("a"), testTool("b")}
	next := []Tool{testTool("a"), testTool("b"), testTool("c")}
	changes := GetToolStateChanges(prev, next)
	if len(changes.ToolsAdded) != 1 || changes.ToolsAdded[0].Name != "c" {
		t.Fatalf("added = %v", changes.ToolsAdded)
	}
	if len(changes.ToolsRemoved) != 0 {
		t.Fatalf("removed = %v", changes.ToolsRemoved)
	}

	changed := []Tool{testTool("a"), {Name: "b", Description: "different", Parameters: json.RawMessage(`{"type":"object"}`)}}
	changes = GetToolStateChanges(prev, changed)
	if len(changes.ToolsAdded) != 1 || changes.ToolsAdded[0].Name != "b" {
		t.Fatalf("added = %v; want b", changes.ToolsAdded)
	}
	if len(changes.ToolsRemoved) != 1 || changes.ToolsRemoved[0].Name != "b" {
		t.Fatalf("removed = %v; want b", changes.ToolsRemoved)
	}

	// A changed definition mid-transcript is a redefinition.
	messages := []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "x"}, ToolsAdded: []Tool{testTool("a")}, Timestamp: 0},
		&SystemMessage{Content: StringOrBlocks{Text: "y"},
			ToolsAdded: []Tool{{Name: "a", Description: "other", Parameters: json.RawMessage(`{"type":"object"}`)}},
			Timestamp:  1},
	}
	if !HasToolRedefinitions(messages) {
		t.Fatal("expected redefinitions")
	}
	if !HasNonAdditiveToolChanges(messages) {
		t.Fatal("expected non-additive changes")
	}

	// Declared tools keep the latest definition in first-declaration order.
	declared := GetDeclaredTools(messages)
	if len(declared) != 1 || declared[0].Description != "other" {
		t.Fatalf("declared = %+v", declared)
	}
	if !reflect.DeepEqual(DeclarationsEqual(testTool("a"), testTool("a")), true) {
		t.Fatal("identical declarations should be equal")
	}
	if DeclarationsEqual(testTool("a"), testTool("b")) {
		t.Fatal("different declarations should not be equal")
	}
}

func TestGetCurrentSystemPromptAndSections(t *testing.T) {
	head := &SystemMessage{Content: StringOrBlocks{Text: "base"}, Timestamp: 0}
	head.SetSection("rules", strptr("<rules>old</rules>"))
	head.SetSection("docs", strptr("<docs>read docs</docs>"))
	late := &SystemMessage{Content: StringOrBlocks{Text: "updated guidance"}, Timestamp: 2}
	late.SetSection("rules", strptr("<rules>new rules</rules>"))
	late.SetSection("docs", nil)

	messages := []Message{
		head,
		&UserMessage{Content: StringOrBlocks{Text: "before"}, Timestamp: 1},
		late,
	}
	prompt := GetCurrentSystemPrompt(messages)
	want := "base\n\nupdated guidance\n\n<rules>new rules</rules>"
	if prompt != want {
		t.Fatalf("prompt =\n%q\nwant\n%q", prompt, want)
	}
}

// A section deleted and then re-added moves to the end, like a JS Map.
func TestGetCurrentSystemMessageReaddsDeletedSectionAtEnd(t *testing.T) {
	head := &SystemMessage{Content: StringOrBlocks{Text: "base"}, Timestamp: 0}
	head.SetSection("a", strptr("1"))
	head.SetSection("b", strptr("2"))
	drop := &SystemMessage{Timestamp: 1}
	drop.SetSection("a", nil)
	readd := &SystemMessage{Timestamp: 2}
	readd.SetSection("a", strptr("3"))
	message := GetCurrentSystemMessage([]Message{head, drop, readd})
	if message == nil {
		t.Fatal("no message")
	}
	if got := GetSystemMessageText(message); got != "base\n\n2\n\n3" {
		t.Fatalf("prompt = %q; want %q", got, "base\n\n2\n\n3")
	}
}
