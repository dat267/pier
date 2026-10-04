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
