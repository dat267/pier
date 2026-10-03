package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// Port of harness/live.ts.

func jsonValue(t *testing.T, value any) any {
	t.Helper()
	encoded, err := marshalJSONValue(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func stageLive(t *testing.T, tx *Transaction, conversationID Id, key string, value any) {
	t.Helper()
	live, err := tx.Doc(LiveDoc.Definition, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	live.(map[string]any)[key] = value
}

func liveDraft(t *testing.T, tx *Transaction, conversationID Id) map[string]any {
	t.Helper()
	live, err := tx.Doc(LiveDoc.Definition, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	return live.(map[string]any)
}

func readLive(t *testing.T, session *Session, conversationID Id) map[string]any {
	t.Helper()
	value, ok, err := session.Snapshot(context.Background(), LiveDoc.Definition, conversationID)
	if err != nil || !ok {
		t.Fatalf("live snapshot = %v, %v", ok, err)
	}
	return value.(map[string]any)
}

func TestLiveDocCheckpointWhen(t *testing.T) {
	if LiveDoc.Definition.Kind != "pi.live" || LiveDoc.Definition.Scope != ScopeConversation {
		t.Fatalf("definition = %+v", LiveDoc.Definition)
	}
	checkpoint := LiveDoc.Definition.CheckpointWhen
	if !checkpoint(map[string]any{}, nil, CheckpointInfo{}) {
		t.Fatal("idle state must checkpoint")
	}
	if checkpoint(map[string]any{"generation": map[string]any{"attempt": 1.0}}, nil, CheckpointInfo{}) {
		t.Fatal("a generation must not checkpoint")
	}
	if checkpoint(map[string]any{"tools": []any{map[string]any{"status": ToolSlotRunning}}}, nil, CheckpointInfo{}) {
		t.Fatal("a running tool slot must not checkpoint")
	}
	if !checkpoint(map[string]any{"tools": []any{map[string]any{"status": ToolSlotDone}}}, nil, CheckpointInfo{}) {
		t.Fatal("a done tool slot must checkpoint")
	}
}

func TestEndRunSettlesInputs(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var submissionID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		submission, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeInput, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		submissionID = submission.ID
		stageLive(t, tx, RootConversationID, "run", jsonValue(t, LiveRun{TaskID: 5, Inputs: []Id{submissionID}}))
		stageLive(t, tx, RootConversationID, "generation", jsonValue(t, LiveGeneration{Attempt: 1}))
		stageLive(t, tx, RootConversationID, "tools", []any{jsonValue(t, ToolSlot{CallID: "c1", Name: "bash", Status: ToolSlotRunning})})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reason := "aborted"
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return EndRun(tx, liveDraft(t, tx, RootConversationID), 5, SubmissionSettlement{Status: SubmissionUnanswered, Reason: &reason})
	}); err != nil {
		t.Fatal(err)
	}
	record, _ := storage.Submission(ctx, submissionID)
	if record.Status != SubmissionUnanswered || record.Reason == nil || *record.Reason != "aborted" {
		t.Fatalf("submission = %+v", record)
	}
	live := readLive(t, session, RootConversationID)
	if _, present := live["run"]; present {
		t.Fatalf("run = %+v", live["run"])
	}
	if _, present := live["generation"]; present {
		t.Fatalf("generation = %+v", live["generation"])
	}
	if _, present := live["tools"]; present {
		t.Fatalf("tools = %+v", live["tools"])
	}
}

func TestEndRunIgnoresOtherTask(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var submissionID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		submission, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeInput, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		submissionID = submission.ID
		stageLive(t, tx, RootConversationID, "run", jsonValue(t, LiveRun{TaskID: 5, Inputs: []Id{submissionID}}))
		stageLive(t, tx, RootConversationID, "generation", jsonValue(t, LiveGeneration{Attempt: 1}))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return EndRun(tx, liveDraft(t, tx, RootConversationID), 9, SubmissionSettlement{Status: SubmissionUnanswered})
	}); err != nil {
		t.Fatal(err)
	}
	live := readLive(t, session, RootConversationID)
	if _, present := live["run"]; !present {
		t.Fatal("another task's run must survive")
	}
	if _, present := live["generation"]; present {
		t.Fatal("generation must be removed regardless")
	}
	record, _ := storage.Submission(ctx, submissionID)
	if record.Status != SubmissionQueued {
		t.Fatalf("submission = %+v", record)
	}
}

func TestCompactionStatusHelpers(t *testing.T) {
	live := map[string]any{}
	if err := AddCompactionStatus(live, CompactionStatus{TaskID: 1, Reason: CompactionManual, Blocking: true, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := AddCompactionStatus(live, CompactionStatus{TaskID: 2, Reason: CompactionThreshold}); err != nil {
		t.Fatal(err)
	}
	if status := CompactionStatusOf(live, 2); status == nil || status["reason"] != CompactionThreshold {
		t.Fatalf("status = %+v", status)
	}
	RemoveCompactionStatus(live, 1)
	if CompactionStatusOf(live, 1) != nil {
		t.Fatal("status 1 must be removed")
	}
	RemoveCompactionStatus(live, 2)
	if _, present := live["compactions"]; present {
		t.Fatalf("compactions = %+v", live["compactions"])
	}
}

func TestToolSlotProgress(t *testing.T) {
	slot := map[string]any{
		"callId": "c1", "name": "bash", "status": ToolSlotRunning,
		"output": "out", "droppedBytes": 1.0, "droppedLines": 2.0,
		"details": map[string]any{"a": 1.0}, "diagnostics": []any{map[string]any{"message": "m"}},
	}
	entry := Id(42)
	FinishSlot(slot, &entry)
	if slot["status"] != ToolSlotDone || !jsonIDEquals(slot["entry"], 42) {
		t.Fatalf("slot = %+v", slot)
	}
	for _, key := range []string{"output", "droppedBytes", "droppedLines", "details", "diagnostics"} {
		if _, present := slot[key]; present {
			t.Fatalf("%s = %+v", key, slot[key])
		}
	}
}

func TestConvertPartialAppendsAbortedAssistant(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	partial := &ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "partial"}}, StopReason: ai.StopPending, Timestamp: 5,
	}
	encoded, err := ai.MarshalMessage(partial)
	if err != nil {
		t.Fatal(err)
	}
	var message any
	if err := json.Unmarshal(encoded, &message); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		stageLive(t, tx, RootConversationID, "generation", map[string]any{"attempt": 1.0, "message": message})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Without a generation the conversion is a no-op.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return ConvertPartial(tx, map[string]any{}, RootConversationID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return ConvertPartial(tx, liveDraft(t, tx, RootConversationID), RootConversationID)
	}); err != nil {
		t.Fatal(err)
	}
	page, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: RootConversationID}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, entry := range page.Items {
		if entry.Kind != AssistantEntry.Kind {
			continue
		}
		found++
		message, ok := entry.Model[0].(*ai.AssistantMessage)
		if !ok || message.StopReason != ai.StopAborted {
			t.Fatalf("assistant = %+v", entry.Model[0])
		}
	}
	if found != 1 {
		t.Fatalf("assistant entries = %d", found)
	}
}

func TestSettleSchedulerOutcome(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A tool task's slot finishes without an entry.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		stageLive(t, tx, RootConversationID, "tools", []any{jsonValue(t, ToolSlot{CallID: "c1", Name: "bash", TaskID: idPtr(7), Status: ToolSlotRunning, Output: stringPointer("out")})})
		return SettleSchedulerOutcome(tx, TaskRecord{ID: 7, ConversationID: RootConversationID, Kind: ToolTaskKind}, SchedulerOutcome{Status: OutcomeFaulted})
	}); err != nil {
		t.Fatal(err)
	}
	live := readLive(t, session, RootConversationID)
	slot := ToolSlotOf(live, 7)
	if slot == nil || slot["status"] != ToolSlotDone {
		t.Fatalf("slot = %+v", slot)
	}
	if _, present := slot["output"]; present {
		t.Fatalf("output = %+v", slot["output"])
	}
	// A compaction task's status is removed.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		live := liveDraft(t, tx, RootConversationID)
		if err := AddCompactionStatus(live, CompactionStatus{TaskID: 8}); err != nil {
			return err
		}
		return SettleSchedulerOutcome(tx, TaskRecord{ID: 8, ConversationID: RootConversationID, Kind: CompactionTaskKind}, SchedulerOutcome{Status: OutcomeOrphaned})
	}); err != nil {
		t.Fatal(err)
	}
	if CompactionStatusOf(readLive(t, session, RootConversationID), 8) != nil {
		t.Fatal("status must be removed")
	}
	// A run task settles its inputs, converts the partial, and records the error.
	var submissionID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		submission, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeInput, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		submissionID = submission.ID
		stageLive(t, tx, RootConversationID, "run", jsonValue(t, LiveRun{TaskID: 9, Inputs: []Id{submissionID}}))
		stageLive(t, tx, RootConversationID, "generation", map[string]any{"attempt": 1.0, "message": jsonValue(t, &ai.AssistantMessage{
			Content: ai.ContentList{ai.TextContent{Text: "half"}}, StopReason: ai.StopPending, Timestamp: 1,
		})})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return SettleSchedulerOutcome(tx, TaskRecord{ID: 9, ConversationID: RootConversationID, Kind: RunTaskKind},
			SchedulerOutcome{Status: OutcomeFaulted, Error: &StoredError{Message: "boom"}})
	}); err != nil {
		t.Fatal(err)
	}
	record, _ := storage.Submission(ctx, submissionID)
	if record.Status != SubmissionUnanswered || record.Reason == nil || *record.Reason != OutcomeFaulted {
		t.Fatalf("submission = %+v", record)
	}
	var detail string
	if err := json.Unmarshal(record.Detail, &detail); err != nil || detail != "boom" {
		t.Fatalf("detail = %s (%v)", record.Detail, err)
	}
	live = readLive(t, session, RootConversationID)
	if _, present := live["run"]; present {
		t.Fatalf("run = %+v", live["run"])
	}
}
