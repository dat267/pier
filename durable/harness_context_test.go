package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// Port of harness/context.ts.

func userMessage(text string, timestamp int64) *ai.UserMessage {
	return &ai.UserMessage{Content: ai.StringOrBlocks{Text: text}, Timestamp: timestamp}
}

func assistantWithCall(id, name string, timestamp int64) *ai.AssistantMessage {
	return &ai.AssistantMessage{
		Content: ai.ContentList{ai.ToolCall{ID: id, Name: name}}, StopReason: ai.StopToolUse, Timestamp: timestamp,
	}
}

func toolResult(id, name, text string, timestamp int64) *ai.ToolResultMessage {
	return &ai.ToolResultMessage{
		ToolCallID: id, ToolName: name, Content: ai.UserContentList{ai.TextContent{Text: text}}, Timestamp: timestamp,
	}
}

func contextEntry(id, conversationID Id, head *Id, model []ai.Message, edits []ContextEdit) StorageWrite {
	return StorageWrite{Type: "entry", Entry: &EntryRecord{
		ID: id, ConversationID: conversationID, Kind: "message", Model: model, Head: head, Edits: edits,
	}}
}

func TestCaptureAndDeriveContext(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	root := RootConversationID
	mustCommit(t, storage, conversationWrite(root))
	// e10 (user) -> e11 (head marker pointing at e10) -> e12 (assistant).
	head := Id(10)
	mustCommit(t, storage,
		contextEntry(10, root, nil, []ai.Message{userMessage("one", 1)}, nil),
		contextEntry(11, root, &head, nil, nil),
		contextEntry(12, root, nil, []ai.Message{userMessage("two", 3)}, nil),
	)
	bounds, err := CaptureContextBounds(ctx, storage, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bounds == nil || bounds.Tail != 12 || bounds.Head == nil || bounds.Head.ID != 11 {
		t.Fatalf("bounds = %+v", bounds)
	}
	view, err := DeriveContext(ctx, storage, root, bounds)
	if err != nil {
		t.Fatal(err)
	}
	// The active entries are the head marker followed by the range's non-head
	// entries (the marker's head target is in the range).
	if len(view.Entries) != 3 || view.Entries[0].ID != 11 || view.Entries[1].ID != 10 || view.Entries[2].ID != 12 {
		t.Fatalf("entries = %+v", view.Entries)
	}
	if len(view.Messages) != 2 || view.Messages[0].(*ai.UserMessage).Timestamp != 1 ||
		view.Messages[1].(*ai.UserMessage).Timestamp != 3 {
		t.Fatalf("messages = %+v", view.Messages)
	}
	// Without a head marker the whole range is active.
	mustCommit(t, storage, conversationWrite(50))
	mustCommit(t, storage, contextEntry(100, 50, nil, []ai.Message{userMessage("a", 1)}, nil))
	bounds, err = CaptureContextBounds(ctx, storage, 50, nil)
	if err != nil || bounds.Head != nil {
		t.Fatalf("bounds = %+v, %v", bounds, err)
	}
	view, _ = DeriveContext(ctx, storage, 50, bounds)
	if len(view.Entries) != 1 || view.Entries[0].ID != 100 {
		t.Fatalf("entries = %+v", view.Entries)
	}
	// An empty conversation has no bounds.
	mustCommit(t, storage, conversationWrite(60))
	if bounds, err := CaptureContextBounds(ctx, storage, 60, nil); err != nil || bounds != nil {
		t.Fatalf("empty = %+v, %v", bounds, err)
	}
}

func TestDeriveContextEditsAndStopReasons(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	root := RootConversationID
	mustCommit(t, storage, conversationWrite(root))
	replacement := []ai.Message{userMessage("replaced", 9)}
	omitted := Id(12)
	mustCommit(t, storage,
		contextEntry(10, root, nil, []ai.Message{userMessage("one", 1)}, nil),
		contextEntry(11, root, nil, []ai.Message{&ai.AssistantMessage{StopReason: ai.StopAborted, Timestamp: 2}}, nil),
		contextEntry(12, root, nil, []ai.Message{userMessage("three", 3)}, nil),
		contextEntry(13, root, nil, []ai.Message{userMessage("four", 4)}, []ContextEdit{
			{Target: 10, Action: EditReplace, Messages: replacement},
			{Target: omitted, Action: EditOmit},
		}),
	)
	bounds, _ := CaptureContextBounds(ctx, storage, root, nil)
	view, err := DeriveContext(ctx, storage, root, bounds)
	if err != nil {
		t.Fatal(err)
	}
	// e1 is replaced, e2 (aborted) contributes nothing, e3 is omitted, e4 stays.
	if len(view.Contributions) != 4 {
		t.Fatalf("contributions = %+v", view.Contributions)
	}
	if len(view.Contributions[0]) != 1 || view.Contributions[0][0].(*ai.UserMessage).Timestamp != 9 {
		t.Fatalf("replaced = %+v", view.Contributions[0])
	}
	if len(view.Contributions[1]) != 0 || len(view.Contributions[2]) != 0 {
		t.Fatalf("excluded = %+v", view.Contributions)
	}
	if view.Messages[0].(*ai.UserMessage).Timestamp != 9 {
		t.Fatalf("messages = %+v", view.Messages)
	}
}

func TestCaptureContextBoundsRequiresVisibleEntry(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	mustCommit(t, storage, conversationWrite(RootConversationID))
	missing := Id(999)
	if _, err := CaptureContextBounds(ctx, storage, RootConversationID, &missing); err == nil {
		t.Fatal("an invisible entry must fail")
	}
}

func TestOrderToolResults(t *testing.T) {
	assistant := assistantWithCall("call-1", "bash", 5)
	result := toolResult("call-1", "bash", "ok", 5)
	user := userMessage("hi", 1)
	ordered := OrderToolResults([]ai.Message{user, assistant, result})
	if len(ordered) != 3 || ordered[0] != ai.Message(user) || ordered[1] != ai.Message(assistant) ||
		ordered[2] != ai.Message(result) {
		t.Fatalf("ordered = %+v", ordered)
	}
	// A missing result is synthesized in call order.
	second := assistantWithCall("call-2", "read", 6)
	ordered = OrderToolResults([]ai.Message{assistant, second, result})
	if len(ordered) != 4 {
		t.Fatalf("ordered = %+v", ordered)
	}
	synthesized, ok := ordered[1].(*ai.ToolResultMessage)
	if !ok || !synthesized.IsError || synthesized.ToolCallID != "call-1" {
		t.Fatalf("synthesized = %+v", ordered[1])
	}
	secondSynth, ok := ordered[3].(*ai.ToolResultMessage)
	if !ok || secondSynth.ToolCallID != "call-2" {
		t.Fatalf("second synthesized = %+v", ordered[3])
	}
	var details map[string]any
	if err := json.Unmarshal(synthesized.Details, &details); err != nil || details["reason"] != "missing_result" {
		t.Fatalf("details = %s", synthesized.Details)
	}
	// An unmatched result is dropped.
	stray := toolResult("call-9", "bash", "stray", 7)
	ordered = OrderToolResults([]ai.Message{user, stray})
	if len(ordered) != 1 || ordered[0] != ai.Message(user) {
		t.Fatalf("ordered = %+v", ordered)
	}
}
