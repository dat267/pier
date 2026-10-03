package durable

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of harness/events.ts.

func liveView(live map[string]any) ConversationView {
	return ConversationView{
		Conversation: ConversationRecord{ID: RootConversationID},
		Docs:         map[string]chord.JsonValue{"pi.live": live},
	}
}

func TestTranslateEntryAppended(t *testing.T) {
	entry := EntryRecord{ID: 10, ConversationID: RootConversationID, Kind: "note"}
	publication := CommitPublication{Changes: []CommitChange{{Write: &StorageWrite{Type: "entry", Entry: &entry}}}}
	events := Translate(RootConversationID, liveView(map[string]any{}), liveView(map[string]any{}), nil, publication, map[Id]bool{})
	if len(events) != 1 || events[0].Type != EventEntryAppended || events[0].Entry == nil || events[0].Entry.ID != 10 {
		t.Fatalf("events = %+v", events)
	}
}

func TestTranslateMessageEntry(t *testing.T) {
	entry := EntryRecord{ID: 11, ConversationID: RootConversationID, Kind: "pi.assistant",
		Model: []ai.Message{&ai.AssistantMessage{StopReason: ai.StopStop, Timestamp: 1}}}
	publication := CommitPublication{Changes: []CommitChange{{Write: &StorageWrite{Type: "entry", Entry: &entry}}}}
	events := Translate(RootConversationID, liveView(map[string]any{}), liveView(map[string]any{}), nil, publication, map[Id]bool{})
	if len(events) != 2 || events[0].Type != EventMessageStart || events[1].Type != EventMessageEnd {
		t.Fatalf("events = %+v", events)
	}
	if _, ok := events[0].Message.(*ai.AssistantMessage); !ok {
		t.Fatalf("message = %+v", events[0].Message)
	}
}

func TestTranslateToolStart(t *testing.T) {
	before := liveView(map[string]any{
		"tools": []any{map[string]any{"callId": "c1", "name": "bash", "taskId": 7.0, "status": ToolSlotPending}},
	})
	after := liveView(map[string]any{
		"tools": []any{map[string]any{"callId": "c1", "name": "bash", "taskId": 7.0, "status": ToolSlotRunning}},
	})
	task := TaskRecord{ID: 7, ConversationID: RootConversationID, Kind: ToolTaskKind,
		State: TaskState{Status: TaskRunning, Checkpoint: []byte(`{"arguments":{"x":1}}`)}}
	publication := CommitPublication{Changes: []CommitChange{{Write: &StorageWrite{Type: "task", Task: &task}}}}
	events := Translate(RootConversationID, before, after, nil, publication, map[Id]bool{})
	if len(events) != 1 || events[0].Type != EventToolExecutionStart || events[0].ToolCallID != "c1" ||
		events[0].ToolName != "bash" {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Args["x"] != 1.0 {
		t.Fatalf("args = %+v", events[0].Args)
	}
}

func TestTranslateToolEnd(t *testing.T) {
	before := liveView(map[string]any{
		"tools": []any{map[string]any{"callId": "c1", "name": "bash", "taskId": 7.0, "status": ToolSlotRunning, "output": "out"}},
	})
	entry := EntryRecord{ID: 20, ConversationID: RootConversationID, Kind: "pi.tool-result",
		Model: []ai.Message{&ai.ToolResultMessage{ToolCallID: "c1", ToolName: "bash", Timestamp: 1}}}
	after := liveView(map[string]any{
		"tools": []any{map[string]any{"callId": "c1", "name": "bash", "taskId": 7.0, "status": ToolSlotDone, "entry": 20.0}},
	})
	publication := CommitPublication{Changes: []CommitChange{{Write: &StorageWrite{Type: "entry", Entry: &entry}}}}
	events := Translate(RootConversationID, before, after, nil, publication, map[Id]bool{})
	found := false
	for _, event := range events {
		if event.Type == EventToolExecutionEnd && event.ToolCallID == "c1" && event.Entry != nil && event.Entry.ID == 20 {
			found = true
		}
	}
	if !found {
		t.Fatalf("events = %+v", events)
	}
}

func TestTranslateRunAndTurn(t *testing.T) {
	before := liveView(map[string]any{})
	input := Id(5)
	after := liveView(map[string]any{"run": map[string]any{"taskId": 9.0, "inputs": []any{5.0}}})
	task := TaskRecord{ID: 9, ConversationID: RootConversationID, Kind: RunTaskKind, State: TaskState{Status: TaskRunning}}
	publication := CommitPublication{Changes: []CommitChange{{Write: &StorageWrite{Type: "task", Task: &task}}}}
	events := Translate(RootConversationID, before, after, nil, publication, map[Id]bool{})
	kinds := eventTypes(events)
	if kinds[EventRunStart] != 1 || kinds[EventTurnStart] != 1 {
		t.Fatalf("events = %+v", events)
	}
	for _, event := range events {
		if event.Type == EventRunStart && (len(event.Inputs) != 1 || event.Inputs[0] != input) {
			t.Fatalf("run_start = %+v", event)
		}
	}
	// The terminal generation task ends the turn; a completing hold also ends it once.
	afterEnd := liveView(map[string]any{})
	terminal := TaskRecord{ID: 9, ConversationID: RootConversationID, Kind: RunTaskKind,
		State: TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}}}
	endPublication := CommitPublication{Changes: []CommitChange{{Write: &StorageWrite{Type: "task", Task: &terminal}}}}
	events = Translate(RootConversationID, after, afterEnd, nil, endPublication, map[Id]bool{})
	if eventTypes(events)[EventRunEnd] != 1 || eventTypes(events)[EventTurnEnd] != 1 {
		t.Fatalf("events = %+v", events)
	}
}

func TestTranslateTaskFailed(t *testing.T) {
	message := "boom"
	task := TaskRecord{ID: 9, ConversationID: RootConversationID, Kind: RunTaskKind,
		State: TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeOrphaned, Reason: &message}}}
	publication := CommitPublication{Changes: []CommitChange{{Write: &StorageWrite{Type: "task", Task: &task}}}}
	events := Translate(RootConversationID, liveView(map[string]any{}), liveView(map[string]any{}), nil, publication, map[Id]bool{})
	found := false
	for _, event := range events {
		if event.Type == EventTaskFailed && event.ErrorMessage == "boom" {
			found = true
		}
	}
	if !found {
		t.Fatalf("events = %+v", events)
	}
}

func eventTypes(events []AgentEvent) map[string]int {
	counts := map[string]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	return counts
}

func TestMessageChanges(t *testing.T) {
	message := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.TextContent{Text: "hello"}, ai.ToolCall{ID: "c1", Name: "bash"}},
		StopReason: ai.StopToolUse, Timestamp: 1,
	}
	prefix := delta.Path{"docs", "pi.live", "generation", "message"}
	textDelta := delta.Op{Verb: delta.VerbAppend, Path: append(append(delta.Path{}, prefix...), "content", 0, "text"), Text: " world"}
	changes := messageChanges([]delta.Op{textDelta}, message)
	if len(changes) != 1 || changes[0].Type != MessageTextDelta || changes[0].ContentIndex != 0 || changes[0].Delta != " world" {
		t.Fatalf("changes = %+v", changes)
	}
	arguments := delta.Op{Verb: delta.VerbAppend, Path: append(append(delta.Path{}, prefix...), "content", 1, "arguments", "x"), Text: "1"}
	changes = messageChanges([]delta.Op{arguments}, message)
	if len(changes) != 1 || changes[0].Type != MessageToolcallDelta || len(changes[0].Path) != 1 || changes[0].Path[0] != "x" {
		t.Fatalf("changes = %+v", changes)
	}
	// A whole-block splice starts the blocks it inserts.
	splice := delta.Op{Verb: delta.VerbSplice, Path: append(append(delta.Path{}, prefix...), "content"),
		Index: 0, Remove: 0, Items: []chord.JsonValue{map[string]any{"type": "text", "text": "a"}}}
	changes = messageChanges([]delta.Op{splice}, message)
	if len(changes) != 1 || changes[0].Type != MessageTextStart || changes[0].ContentIndex != 0 {
		t.Fatalf("changes = %+v", changes)
	}
	// Usage-only changes are ignored; a whole-message or generation replacement yields one whole change.
	usageOp := delta.Op{Verb: delta.VerbSet, Path: append(append(delta.Path{}, prefix...), "usage"), Value: 1.0}
	if changes = messageChanges([]delta.Op{usageOp}, message); len(changes) != 0 {
		t.Fatalf("changes = %+v", changes)
	}
	replace := delta.Op{Verb: delta.VerbReplace, Value: map[string]any{}}
	changes = messageChanges([]delta.Op{replace}, message)
	if len(changes) != 1 || changes[0].Type != MessageWhole || changes[0].Message == nil {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestToolUpdate(t *testing.T) {
	slot := ToolSlot{CallID: "c1", Name: "bash", Status: ToolSlotRunning, Output: stringPointer("tail")}
	previous := ToolSlot{CallID: "c1", Name: "bash", Status: ToolSlotRunning, Output: stringPointer("tail")}
	outputPath := delta.Path{"docs", "pi.live", "tools", 0, "output"}
	trim := delta.Op{Verb: delta.VerbTruncate, Path: outputPath, Count: 3}
	appendOp := delta.Op{Verb: delta.VerbAppend, Path: outputPath, Text: "abc"}
	update := toolUpdate([]delta.Op{trim, appendOp}, 0, slot, previous)
	if update == nil || update.output == nil || update.output.TrimStart == nil || *update.output.TrimStart != 3 ||
		update.output.Append == nil || *update.output.Append != "abc" {
		t.Fatalf("update = %+v", update)
	}
	// An output change without an op is a whole replacement.
	slot.Output = stringPointer("new")
	update = toolUpdate(nil, 0, slot, previous)
	if update == nil || update.output == nil || update.output.Set == nil || *update.output.Set != "new" {
		t.Fatalf("update = %+v", update)
	}
	// Removed details and diagnostics replay as null and [].
	previous.Details = map[string]any{"a": 1.0}
	previous.Diagnostics = []ToolDiagnostic{{Severity: DiagnosticWarn, Message: "m"}}
	slot.Details = nil
	slot.Diagnostics = nil
	update = toolUpdate(nil, 0, slot, previous)
	if update == nil || !update.detailsSet || update.details != nil || !update.diagnosticsSet ||
		len(update.diagnostics) != 0 {
		t.Fatalf("update = %+v", update)
	}
}

func TestWatchEventsSnapshotAndBatch(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if _, err := tx.CreateRootConversation(); err != nil {
			return err
		}
		return Configure(tx, RootConversationID, AgentChange{Instructions: SetOf("agent")})
	}); err != nil {
		t.Fatal(err)
	}
	views := NewConversationViews(session, storage)
	_ = views
	stream, err := WatchEvents(session, RootConversationID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stream.Snapshot.Type != EventSnapshot || len(stream.Snapshot.Entries) != 0 {
		t.Fatalf("snapshot = %+v", stream.Snapshot)
	}
	if stream.Snapshot.Agent == nil || stream.Snapshot.Agent.Instructions == nil || *stream.Snapshot.Agent.Instructions != "agent" {
		t.Fatalf("agent = %+v", stream.Snapshot.Agent)
	}
	batches := make(chan []AgentEvent, 4)
	stream.Start(func(events []AgentEvent, ctx chord.Context) error {
		batches <- events
		return nil
	})
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "note"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case events := <-batches:
		if eventTypes(events)[EventEntryAppended] != 1 {
			t.Fatalf("events = %+v", events)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no batch")
	}
	end := stream.Stop()
	if end.Reason != WatchReasonStopped {
		t.Fatalf("end = %+v", end)
	}
}
