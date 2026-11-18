package coding

import (
	"encoding/json"
	"github.com/dat267/pier/ai"
	"strings"
	"testing"
)

// Compaction tests keyed to upstream compaction.ts semantics.

func TestFileOperationsAndSerialization(t *testing.T) {
	fileOps := CreateFileOps()
	ExtractFileOpsFromMessage(&ai.AssistantMessage{
		Content: ai.ContentList{
			ai.ToolCall{ID: "1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			ai.ToolCall{ID: "2", Name: "read", Arguments: json.RawMessage(`{"path":"b.txt"}`)},
			ai.ToolCall{ID: "3", Name: "write", Arguments: json.RawMessage(`{"path":"b.txt"}`)},
			ai.ToolCall{ID: "4", Name: "edit", Arguments: json.RawMessage(`{"path":"c.txt"}`)},
		},
	}, fileOps)

	readFiles, modifiedFiles := ComputeFileLists(fileOps)
	// b.txt was read then written → modified only. a.txt stays read-only.
	if len(readFiles) != 1 || readFiles[0] != "a.txt" {
		t.Fatalf("readFiles = %v", readFiles)
	}
	if len(modifiedFiles) != 2 || modifiedFiles[0] != "b.txt" || modifiedFiles[1] != "c.txt" {
		t.Fatalf("modifiedFiles = %v", modifiedFiles)
	}

	formatted := FormatFileOperations(readFiles, modifiedFiles)
	if !strings.Contains(formatted, "<read-files>\na.txt\n</read-files>") ||
		!strings.Contains(formatted, "<modified-files>\nb.txt\nc.txt\n</modified-files>") {
		t.Fatalf("formatted = %q", formatted)
	}
	if FormatFileOperations(nil, nil) != "" {
		t.Fatal("empty ops render empty")
	}
}

func TestFileOperationsFromNestedToolCalls(t *testing.T) {
	// Upstream extractFileOpsFromMessage reads the nested calls recorded on a
	// tool result, which is how codemode script file access reaches the
	// compaction file lists. A byte-heavy call arrives without arguments, so it
	// must be skipped rather than contributing an empty path.
	fileOps := CreateFileOps()
	ExtractFileOpsFromMessage(&ai.ToolResultMessage{
		ToolCallID: "c1", ToolName: "bash",
		NestedCalls: &ai.NestedToolCalls{Complete: true, Calls: []ai.NestedToolCallRecord{
			{ID: "n1", Name: "read", Arguments: json.RawMessage(`{"path":"script-read.txt"}`)},
			{ID: "n2", Name: "write", Arguments: json.RawMessage(`{"path":"script-write.txt"}`)},
			{ID: "n3", Name: "edit", Arguments: json.RawMessage(`{"path":"script-edit.txt"}`)},
			{ID: "n4", Name: "read", ArgumentsBytes: new(9000)},
		}},
	}, fileOps)

	readFiles, modifiedFiles := ComputeFileLists(fileOps)
	if len(readFiles) != 1 || readFiles[0] != "script-read.txt" {
		t.Fatalf("readFiles = %v", readFiles)
	}
	if len(modifiedFiles) != 2 || modifiedFiles[0] != "script-edit.txt" || modifiedFiles[1] != "script-write.txt" {
		t.Fatalf("modifiedFiles = %v", modifiedFiles)
	}
	// A tool result without nested calls contributes nothing.
	empty := CreateFileOps()
	ExtractFileOpsFromMessage(&ai.ToolResultMessage{ToolCallID: "c2", ToolName: "read"}, empty)
	if len(empty.Read)+len(empty.Written)+len(empty.Edited) != 0 {
		t.Fatalf("plain tool result added file ops: %+v", empty)
	}
}

func TestSerializeConversation(t *testing.T) {
	messages := []ai.Message{
		createUserMessage("do the thing"),
		&ai.AssistantMessage{
			Content: ai.ContentList{
				ai.ThinkingContent{Thinking: "pondering"},
				ai.ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			},
			API: "openai-responses", Provider: "openai", Model: "mock",
			StopReason: ai.StopToolUse, Timestamp: 2,
		},
		&ai.ToolResultMessage{
			ToolCallID: "c1", ToolName: "read",
			Content:   ai.UserContentList{ai.TextContent{Text: strings.Repeat("r", 3000)}},
			Timestamp: 3,
		},
	}
	serialized := SerializeConversation(ConvertToLlm(messages))
	if !strings.Contains(serialized, "[User]: do the thing") {
		t.Fatalf("user missing: %q", serialized)
	}
	if strings.Contains(serialized, "pondering") || strings.Contains(serialized, "[Assistant thinking]") {
		t.Fatalf("thinking should not be serialized for compaction: %q", serialized)
	}
	if !strings.Contains(serialized, `[Assistant tool calls]: read(path="a.txt")`) {
		t.Fatalf("tool calls: %q", serialized)
	}
	// Tool results truncate at 2000 chars with the marker.
	if !strings.Contains(serialized, "[... 1000 more characters truncated]") {
		t.Fatalf("truncation marker missing: %q", serialized[len(serialized)-120:])
	}
}

func TestConvertToLlmCustomRoles(t *testing.T) {
	bashMsg := CreateBashExecutionMessage("ls -la", "file1", new(0), false, false, "", false, 100, nil)
	excluded := CreateBashExecutionMessage("secret", "", new(0), false, false, "", true, 101, nil)
	compacted := CreateCompactionSummaryMessage("the summary", 1000, 102)
	branch := CreateBranchSummaryMessage("branch summary", "root", 103)
	messages := []ai.Message{createUserMessage("hi"), bashMsg, excluded, compacted, branch}

	converted := ConvertToLlm(messages)
	roles := messageRoles(converted)
	// user, user(bash), user(compaction summary), user(branch summary);
	// excluded dropped.
	if len(roles) != 4 {
		t.Fatalf("roles = %v", roles)
	}
	for i := 1; i < 4; i++ {
		if roles[i] != "user" {
			t.Fatalf("role %d = %s; all custom roles convert to user", i, roles[i])
		}
	}
	// Compaction summary renders with the prefix/suffix wrapper.
	if user, ok := converted[2].(*ai.UserMessage); ok {
		text := user.Content.Blocks[0].(ai.TextContent).Text
		if !strings.HasPrefix(text, CompactionSummaryPrefix) || !strings.HasSuffix(text, CompactionSummarySuffix) {
			t.Fatalf("compaction text = %q", text)
		}
	}
	// Bash execution rendering.
	if user, ok := converted[1].(*ai.UserMessage); ok {
		text := user.Content.Blocks[0].(ai.TextContent).Text
		if !strings.HasPrefix(text, "Ran `ls -la`") || !strings.Contains(text, "```\nfile1\n```") {
			t.Fatalf("bash text = %q", text)
		}
	}
}

// Upstream serializes tool-call arguments with Object.entries, preserving the
// model's key order; the port sorted them.
func TestSerializeConversationPreservesToolArgOrder(t *testing.T) {
	assistant := &ai.AssistantMessage{
		Content: ai.ContentList{
			ai.ToolCall{ID: "c1", Name: "edit", Arguments: json.RawMessage(`{"b":2,"a":1}`)},
		},
		API: "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopToolUse, Timestamp: 2,
	}
	serialized := SerializeConversation(ConvertToLlm([]ai.Message{assistant}))
	if !strings.Contains(serialized, `edit(b=2, a=1)`) {
		t.Fatalf("tool calls = %q; want insertion order", serialized)
	}
	// Nested object keys keep their order too.
	nested := &ai.AssistantMessage{
		Content: ai.ContentList{
			ai.ToolCall{ID: "c2", Name: "edit", Arguments: json.RawMessage(`{"path":"x","edits":[{"oldText":"a","newText":"b"}]}`)},
		},
		API: "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopToolUse, Timestamp: 2,
	}
	serialized = SerializeConversation(ConvertToLlm([]ai.Message{nested}))
	if !strings.Contains(serialized, `edit(path="x", edits=[{"oldText":"a","newText":"b"}])`) {
		t.Fatalf("nested tool calls = %q; want nested insertion order", serialized)
	}
}

// Upstream contentText joins assistant text blocks with newlines.
func TestSerializeConversationJoinsAssistantTextWithNewlines(t *testing.T) {
	assistant := &ai.AssistantMessage{
		Content: ai.ContentList{
			ai.TextContent{Text: "line1"},
			ai.TextContent{Text: "line2"},
		},
		API: "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopStop, Timestamp: 2,
	}
	serialized := SerializeConversation(ConvertToLlm([]ai.Message{assistant}))
	if !strings.Contains(serialized, "[Assistant]: line1\nline2") {
		t.Fatalf("assistant text = %q; want newline join", serialized)
	}
}
