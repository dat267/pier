package durable

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Port of the remaining task-definition metadata and pure helpers
// (compaction.ts createCompaction/thresholdCompaction, generation.ts
// createToolTask, tool.ts fromSlot/invalid).

func TestTaskDefinitionInitials(t *testing.T) {
	initial, err := ToolTask.Definition.Initial(nil)
	if err != nil || strings.TrimSpace(string(initial)) != `{"phase":"call"}` {
		t.Fatalf("tool initial = %s, %v", initial, err)
	}
	initial, err = CompactionTask.Definition.Initial(nil)
	if err != nil || strings.TrimSpace(string(initial)) != `{"phase":"select"}` {
		t.Fatalf("compaction initial = %s, %v", initial, err)
	}
}

func thresholdView() ContextView {
	big := &ai.UserMessage{Content: ai.StringOrBlocks{Text: strings.Repeat("x", 2800)}}
	second := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}
	contributions := [][]ai.Message{{big}, {second}}
	return compactionView(nil, []EntryRecord{{ID: 1}, {ID: 2}}, contributions)
}

func TestThresholdCompaction(t *testing.T) {
	view := thresholdView()
	tokens := EstimateContext(view, nil)
	if tokens <= 0 {
		t.Fatal("expected positive tokens")
	}
	// Blocking when the context exceeds the reserve-adjusted window.
	blocking := CompactionPolicy{Enabled: true, ReserveTokens: 1, KeepRecentTokens: 1}
	if got := ThresholdCompaction(view, nil, tokens, blocking); got != ThresholdBlocking {
		t.Fatalf("blocking = %q", got)
	}
	// Background between the background and blocking thresholds.
	background := CompactionPolicy{Enabled: true, ReserveTokens: 1, KeepRecentTokens: 1, BackgroundTokens: 150}
	if got := ThresholdCompaction(view, nil, tokens+100, background); got != ThresholdBackground {
		t.Fatalf("background = %q", got)
	}
	// Disabled or short context compacts nothing.
	if got := ThresholdCompaction(view, nil, tokens, CompactionPolicy{Enabled: false}); got != "" {
		t.Fatalf("disabled = %q", got)
	}
	if got := ThresholdCompaction(view, nil, 0, blocking); got != "" {
		t.Fatalf("no window = %q", got)
	}
	// No cut means nothing to compact.
	head := EntryRecord{ID: 1}
	assistant := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: strings.Repeat("x", 2800)}}}
	noCut := compactionView(&head, []EntryRecord{{ID: 1}, {ID: 2}}, [][]ai.Message{{assistant}, {&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}}}})
	if got := ThresholdCompaction(noCut, nil, tokens, blocking); got != "" {
		t.Fatalf("no cut = %q", got)
	}
	// Planned messages count toward the size.
	if got := ThresholdCompaction(view, []EntryDraft{{Model: []ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Text: strings.Repeat("y", 8000)}}}}}, tokens, blocking); got != ThresholdBlocking {
		t.Fatalf("planned = %q", got)
	}
}

func TestCreateCompaction(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var manual Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		manual, err = CreateCompaction(tx, RootConversationID, CompactionInput{Reason: CompactionManual}, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, err := storage.Task(ctx, manual)
	if err != nil || record == nil || record.Kind != CompactionTaskKind || record.Background || record.Owner != nil {
		t.Fatalf("record = %+v, %v", record, err)
	}
	status := CompactionStatusOf(readLive(t, session, RootConversationID), manual)
	if status == nil || status["blocking"] != false || status["reason"] != CompactionManual {
		t.Fatalf("status = %+v", status)
	}
	// A threshold compaction is background; an owned one is blocking.
	var threshold, owned Id
	var owner Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		owner, err = CreateGeneration(tx, RootConversationID)
		if err != nil {
			return err
		}
		threshold, err = CreateCompaction(tx, RootConversationID, CompactionInput{Reason: CompactionThreshold}, nil)
		if err != nil {
			return err
		}
		owned, err = CreateCompaction(tx, RootConversationID, CompactionInput{Reason: CompactionManual}, &owner)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	thresholdRecord, _ := storage.Task(ctx, threshold)
	if thresholdRecord == nil || !thresholdRecord.Background {
		t.Fatalf("threshold = %+v", thresholdRecord)
	}
	ownedRecord, _ := storage.Task(ctx, owned)
	if ownedRecord == nil || ownedRecord.Owner == nil || *ownedRecord.Owner != owner {
		t.Fatalf("owned = %+v", ownedRecord)
	}
	status = CompactionStatusOf(readLive(t, session, RootConversationID), owned)
	if status == nil || status["blocking"] != true {
		t.Fatalf("owned status = %+v", status)
	}
}

func TestCreateToolTask(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var owner, toolTask Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		owner, err = CreateGeneration(tx, RootConversationID)
		if err != nil {
			return err
		}
		toolTask, err = CreateToolTask(tx, owner, 42, "c1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, err := storage.Task(ctx, toolTask)
	if err != nil || record == nil || record.Kind != ToolTaskKind || record.ConversationID != RootConversationID {
		t.Fatalf("record = %+v, %v", record, err)
	}
	if record.Owner == nil || *record.Owner != owner {
		t.Fatalf("owner = %+v", record.Owner)
	}
}

func TestFromSlotAndInvalidArguments(t *testing.T) {
	slot := map[string]any{
		"callId": "c1", "name": "bash", "status": ToolSlotDone,
		"output": "partial", "droppedBytes": 10.0, "droppedLines": 2.0,
		"details":     map[string]any{"a": 1.0},
		"diagnostics": []any{map[string]any{"severity": DiagnosticWarn, "message": "m"}},
	}
	result := FromSlot(slot, "interrupted", "Tool bash was interrupted")
	if result.IsError == nil || !*result.IsError {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != "partial" {
		t.Fatalf("content = %+v", result.Content)
	}
	if result.Details == nil {
		t.Fatalf("details = %+v", result.Details)
	}
	if len(result.Diagnostics) != 3 {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
	if result.Diagnostics[1].Code == nil || *result.Diagnostics[1].Code != "truncated" {
		t.Fatalf("truncated = %+v", result.Diagnostics[1])
	}
	if result.Diagnostics[2].Code == nil || *result.Diagnostics[2].Code != "interrupted" {
		t.Fatalf("code = %+v", result.Diagnostics[2])
	}
	// An empty slot yields the code diagnostic only.
	empty := FromSlot(nil, "aborted", "aborted")
	if len(empty.Content) != 0 || len(empty.Diagnostics) != 1 || empty.Diagnostics[0].Code == nil ||
		*empty.Diagnostics[0].Code != "aborted" {
		t.Fatalf("empty = %+v", empty)
	}
	invalid := InvalidArguments("bad args")
	if len(invalid.Diagnostics) != 1 || invalid.Diagnostics[0].Code == nil ||
		*invalid.Diagnostics[0].Code != "invalid_arguments" {
		t.Fatalf("invalid = %+v", invalid)
	}
}
