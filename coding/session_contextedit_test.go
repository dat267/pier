package coding

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// TestContextEditProjection covers the context_edit entry: a replacement swaps
// the target's content for model context, a null replacement omits it.
func TestContextEditProjection(t *testing.T) {
	manager := newTestSessionManager(t)
	messageID := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "original"}, Timestamp: 1})
	if _, err := manager.AppendContextEdit(messageID, json.RawMessage(`{"content":"replaced"}`)); err != nil {
		t.Fatal(err)
	}
	context := manager.Projection()
	// The context_edit entry itself yields no message.
	if len(context.Messages) != 1 {
		t.Fatalf("messages = %+v", context.Messages)
	}
	user, ok := context.Messages[0].(*ai.UserMessage)
	if !ok || user.Content.Text != "replaced" {
		t.Fatalf("message = %+v", context.Messages[0])
	}

	// A null replacement omits the target entirely.
	omitting := newTestSessionManager(t)
	gone := omitting.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "gone"}, Timestamp: 1})
	if _, err := omitting.AppendContextEdit(gone, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	if messages := omitting.Projection().Messages; len(messages) != 0 {
		t.Fatalf("null edit kept the target: %+v", messages)
	}
}

// TestContextEditAssistantStringWraps pins the normalization: an assistant
// string replacement is stored as a text block.
func TestContextEditAssistantStringWraps(t *testing.T) {
	manager := newTestSessionManager(t)
	messageID := manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "original"}},
		API:     ai.APIOpenAIResponses, Provider: "openai", Model: "m",
		Usage: ai.Usage{Cost: ai.UsageCost{}}, StopReason: ai.StopStop, Timestamp: 1,
	})
	if _, err := manager.AppendContextEdit(messageID, json.RawMessage(`{"content":"replaced"}`)); err != nil {
		t.Fatal(err)
	}
	messages := manager.Projection().Messages
	if len(messages) != 1 {
		t.Fatalf("messages = %+v", messages)
	}
	assistant, ok := messages[0].(*ai.AssistantMessage)
	if !ok || len(assistant.Content) != 1 {
		t.Fatalf("message = %+v", messages[0])
	}
	if text, ok := assistant.Content[0].(ai.TextContent); !ok || text.Text != "replaced" {
		t.Fatalf("content = %+v", assistant.Content)
	}
}

// TestAppendContextEditValidation covers the input rejections.
func TestAppendContextEditValidation(t *testing.T) {
	manager := newTestSessionManager(t)
	messageID := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}, Timestamp: 1})
	if _, err := manager.AppendContextEdit(messageID, json.RawMessage(`{"content":42}`)); err == nil {
		t.Fatal("a numeric replacement must be rejected")
	}
	if _, err := manager.AppendContextEdit("missing", json.RawMessage("null")); err == nil {
		t.Fatal("a missing target must be rejected")
	}
	labelID, err := manager.AppendLabelChange(messageID, strPtrForContextEdit("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AppendContextEdit(labelID, json.RawMessage("null")); err == nil {
		t.Fatal("a label must not be editable")
	}
}

func strPtrForContextEdit(value string) *string { return &value }

// TestContextEditEntryRoundTrips pins that a context_edit line keeps its
// replacement through load and rewrite.
func TestContextEditEntryRoundTrips(t *testing.T) {
	line := `{"type":"context_edit","id":"e1","timestamp":"2026-01-01T00:00:00Z","targetId":"m1","replacement":{"content":"x"}}`
	entry, err := UnmarshalFileEntry(line)
	if err != nil || entry.Entry == nil {
		t.Fatalf("entry = %+v, %v", entry, err)
	}
	encoded, err := MarshalFileEntry(*entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded, `"replacement":{"content":"x"}`) {
		t.Fatalf("replacement lost: %s", encoded)
	}
	// A null replacement also survives.
	nullEntry, err := UnmarshalFileEntry(`{"type":"context_edit","id":"e2","targetId":"m1","replacement":null}`)
	if err != nil {
		t.Fatal(err)
	}
	nullEncoded, err := MarshalFileEntry(*nullEntry)
	if err != nil || !strings.Contains(nullEncoded, `"replacement":null`) {
		t.Fatalf("null replacement lost: %s (%v)", nullEncoded, err)
	}
}

// TestProjectedContextEstimateIgnoresOmittedMessages covers the edit-aware
// token accounting (upstream estimateProjectedContextTokens): an omitted
// message is not part of the context, so it must not count toward the
// compaction threshold or the reported context usage.
func TestProjectedContextEstimateIgnoresOmittedMessages(t *testing.T) {
	manager := newTestSessionManager(t)
	manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.ToolCall{ID: "c1", Name: "bash", Arguments: []byte("{}")}},
		API:     ai.APIOpenAIResponses, Provider: "openai", Model: "m", StopReason: ai.StopToolUse, Timestamp: 1,
	})
	// 40k characters is 10k estimated tokens; the message is then omitted.
	omitted := manager.AppendMessage(&ai.ToolResultMessage{
		ToolCallID: "c1", ToolName: "bash",
		Content:   ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", 40000)}},
		Timestamp: 2,
	})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "next"}, Timestamp: 3})
	raw := EstimateContextTokens(manager.Projection().Messages)
	if _, err := manager.AppendContextEdit(omitted, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}

	projected := manager.ProjectedContextEstimate()
	// The remaining context is the tool call plus the user message: a handful of
	// tokens, not the 10k the omitted result used to cost.
	if projected.Tokens > 100 {
		t.Fatalf("projected tokens = %d; the omitted result still counts", projected.Tokens)
	}
	if raw.Tokens <= projected.Tokens {
		t.Fatalf("raw estimate %d did not exceed the projected %d", raw.Tokens, projected.Tokens)
	}
	// With no usage in the projection the estimate is a pure message-size sum.
	if projected.LastUsageIndex != -1 {
		t.Fatalf("last usage index = %d, want -1", projected.LastUsageIndex)
	}
}

// TestProjectedContextEstimateInvalidatesStaleUsage covers the other half of
// the rule: usage from before a context edit describes an older context, so the
// estimate falls back to message sizes instead of trusting it.
func TestProjectedContextEstimateInvalidatesStaleUsage(t *testing.T) {
	manager := newTestSessionManager(t)
	first := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}, Timestamp: 1})
	manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "reply"}},
		API:     ai.APIOpenAIResponses, Provider: "openai", Model: "m", StopReason: ai.StopStop, Timestamp: 2,
		Usage: ai.Usage{Input: 900000, Output: 100, TotalTokens: 900100},
	})

	// Before the edit the provider usage is trustworthy and is used as is.
	if estimate := manager.ProjectedContextEstimate(); estimate.Tokens != 900100 {
		t.Fatalf("pre-edit tokens = %d, want the usage-backed 900100", estimate.Tokens)
	}

	if _, err := manager.AppendContextEdit(first, json.RawMessage(`{"content":"short"}`)); err != nil {
		t.Fatal(err)
	}
	estimate := manager.ProjectedContextEstimate()
	if estimate.Tokens >= 900100 {
		t.Fatalf("post-edit tokens = %d; stale usage was trusted", estimate.Tokens)
	}
	if estimate.LastUsageIndex != -1 || estimate.UsageTokens != 0 {
		t.Fatalf("post-edit estimate = %+v, want a pure message-size estimate", estimate)
	}
}

// TestProjectedWindowMatchesSessionContext pins the provenance-bearing window
// against the projection the session hands out: the two are separate code
// paths and must agree on what each entry contributes.
func TestProjectedWindowMatchesSessionContext(t *testing.T) {
	manager := newTestSessionManager(t)
	first := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}, Timestamp: 1})
	manager.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "reply"}}, API: ai.APIOpenAIResponses,
		Provider: "openai", Model: "m", StopReason: ai.StopStop, Timestamp: 2,
	})
	kept := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "kept"}, Timestamp: 3})
	manager.AppendCompaction("earlier summary", kept, 10, nil, false, nil)
	if _, err := manager.AppendContextEdit(first, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "after"}, Timestamp: 4})

	context := manager.Projection()
	window := projectWindow(materializePointers(manager.GetBranch("")), &manager.messages)
	if len(window.Messages) != len(context.Messages) {
		t.Fatalf("window carries %d messages, projection %d", len(window.Messages), len(context.Messages))
	}
	for index := range window.Messages {
		if ai.RoleOf(window.Messages[index]) != ai.RoleOf(context.Messages[index]) {
			t.Fatalf("message %d: window role %s, projection role %s",
				index, ai.RoleOf(window.Messages[index]), ai.RoleOf(context.Messages[index]))
		}
	}
	if len(context.Entries) != len(window.Entries) {
		t.Fatalf("window carries %d entries, projection %d", len(window.Entries), len(context.Entries))
	}
	for index := range window.Entries {
		if window.Entries[index].Source.ID != context.Entries[index].ID {
			t.Fatalf("entry %d: window %s, projection %s",
				index, window.Entries[index].Source.ID, context.Entries[index].ID)
		}
	}
	if !manager.HasContextEdits() {
		t.Fatal("HasContextEdits missed the appended edit")
	}
}

// Reference: pi v1.3.0 core/session-manager.ts projectContextEntry replaces
// content only for user, assistant, toolResult and the literal "custom" role.
func TestContextEditIgnoresOtherCustomAgentRoles(t *testing.T) {
	messages := []ai.Message{
		CreateCompactionSummaryMessage("summary", 100, 1),
		CreateBranchSummaryMessage("branch", "from", 1),
		&ai.CustomMessage{Role: RoleBashExecution, Content: json.RawMessage(`{"role":"bashExecution","command":"echo hi","output":"hi","timestamp":1}`), Timestamp: 1},
	}
	for _, message := range messages {
		before, err := ai.MarshalMessage(message)
		if err != nil {
			t.Fatal(err)
		}
		after := applyContextEdit([]ai.Message{message}, json.RawMessage(`{"content":"replacement"}`))
		if len(after) != 1 {
			t.Fatalf("messages=%d", len(after))
		}
		encoded, err := ai.MarshalMessage(after[0])
		if err != nil || string(encoded) != string(before) {
			t.Fatalf("role %s changed: %s -> %s (%v)", ai.RoleOf(message), before, encoded, err)
		}
	}
}

// projectContextEntry uses object spread, preserving existing property order
// while replacing content. A map round-trip reordered flat custom messages.
func TestContextEditPreservesCustomMessageMemberOrder(t *testing.T) {
	message := CreateCustomMessage("note", json.RawMessage(`"before"`), true, json.RawMessage(`{"z":1,"a":2}`), 1)
	edited := applyContextEdit([]ai.Message{message}, json.RawMessage(`{"content":"after"}`))
	got, err := ai.MarshalMessage(edited[0])
	want := `{"role":"custom","customType":"note","content":"after","display":true,"details":{"z":1,"a":2},"timestamp":1}`
	if err != nil || string(got) != want {
		t.Fatalf("message=%s err=%v want=%s", got, err, want)
	}
}
