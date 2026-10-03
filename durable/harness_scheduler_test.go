package durable

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Port of the pure scheduler helpers (harness/scheduler.ts).

func TestIsLiveTaskStatus(t *testing.T) {
	for _, status := range []string{TaskPending, TaskRunning, TaskWaiting, TaskCompleting} {
		if !IsLiveTaskStatus(status) {
			t.Fatalf("%s must be live", status)
		}
	}
	if IsLiveTaskStatus(TaskTerminal) {
		t.Fatal("terminal must not be live")
	}
}

func TestCancellationIntentAndFailedOutcome(t *testing.T) {
	running := TaskRecord{State: TaskState{Status: TaskRunning}}
	if CancellationIntent(running) || FailedOutcome(running) {
		t.Fatal("a plain running task has no intent")
	}
	marked := running
	marked.AbortRequested = true
	if !CancellationIntent(marked) {
		t.Fatal("an abort mark is intent")
	}
	completing := TaskRecord{State: TaskState{Status: TaskCompleting, Outcome: &TaskOutcome{Status: OutcomeFailed}}}
	if !FailedOutcome(completing) || !CancellationIntent(completing) {
		t.Fatal("a failed completing outcome is intent")
	}
	completed := TaskRecord{State: TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}}}
	if FailedOutcome(completed) || CancellationIntent(completed) {
		t.Fatal("a completed terminal task has no intent")
	}
}

func TestNodeOfAndParentOf(t *testing.T) {
	record := TaskRecord{ConversationID: 3, Background: true}
	node := NodeOf(record)
	if node.ConversationID != 3 || node.Owner != nil || !node.Background {
		t.Fatalf("node = %+v", node)
	}
	up := ParentOf(node)
	if up.Conversation == nil || *up.Conversation != 3 || up.Task != nil {
		t.Fatalf("up = %+v", up)
	}
	owner := Id(7)
	record.Owner = &owner
	node = NodeOf(record)
	up = ParentOf(node)
	if up.Task == nil || *up.Task != 7 || up.Conversation != nil {
		t.Fatalf("up = %+v", up)
	}
}

func TestOverlayOf(t *testing.T) {
	session, _ := newRootSession(t)
	ctx := context.Background()
	var taskID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		taskID, err = CreateGeneration(tx, RootConversationID)
		if err != nil {
			return err
		}
		child, err := tx.CreateConversation(ConversationOwnership{Kind: ConversationOwnerless})
		if err != nil {
			return err
		}
		overlay := OverlayOf(tx)
		if record, present := overlay.Tasks[taskID]; !present || record.Kind != RunTaskKind {
			t.Fatalf("tasks = %+v", overlay.Tasks)
		}
		if owner, present := overlay.Edges[child.ID]; !present || owner != nil {
			t.Fatalf("edges = %+v", overlay.Edges)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMemoOf(t *testing.T) {
	record := TaskRecord{Memos: map[string]json.RawMessage{"note": json.RawMessage(`1`)}}
	if value, present := MemoOf(&record, "note"); !present || string(value) != "1" {
		t.Fatalf("memo = %s, %v", value, present)
	}
	if _, present := MemoOf(&record, "missing"); present {
		t.Fatal("a missing memo must not be present")
	}
	if _, present := MemoOf(nil, "note"); present {
		t.Fatal("a nil record has no memo")
	}
}

func TestCanReserveAndMigration(t *testing.T) {
	record := TaskRecord{Kind: "x", Version: 1}
	migrating := Task{Definition: TaskDefinition{Name: "x", Version: 2,
		Migrate: func(input, checkpoint json.RawMessage, from int) (json.RawMessage, json.RawMessage, error) {
			return input, checkpoint, nil
		}}}
	if !CanReserve(migrating, record) {
		t.Fatal("a newer version with a migration can reserve")
	}
	newer := Task{Definition: TaskDefinition{Name: "x", Version: 2}}
	if CanReserve(newer, record) {
		t.Fatal("a newer version without a migration cannot reserve")
	}
	same := Task{Definition: TaskDefinition{Name: "x", Version: 1}}
	if !CanReserve(same, record) {
		t.Fatal("the same version can reserve")
	}
	older := Task{Definition: TaskDefinition{Name: "x", Version: 0}}
	if CanReserve(older, record) {
		t.Fatal("an older version cannot reserve")
	}
	if err := MissingMigration(record, newer.Definition); err == nil {
		t.Fatal("a missing migration is an error")
	}
}

func TestWithStateClearsMemosOnOutcome(t *testing.T) {
	record := TaskRecord{Memos: map[string]json.RawMessage{"note": json.RawMessage(`1`)}}
	kept := WithState(record, TaskState{Status: TaskRunning})
	if kept.Memos == nil {
		t.Fatal("a running state keeps memos")
	}
	cleared := WithState(record, TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}})
	if cleared.Memos != nil {
		t.Fatalf("memos = %+v", cleared.Memos)
	}
	completing := WithState(record, TaskState{Status: TaskCompleting, Outcome: &TaskOutcome{Status: OutcomeFailed}})
	if completing.Memos != nil {
		t.Fatal("a completing state clears memos")
	}
}

func TestJSONEqual(t *testing.T) {
	left := map[string]any{"a": 1.0, "b": []any{"x", map[string]any{"c": true}}}
	right := map[string]any{"b": []any{"x", map[string]any{"c": true}}, "a": 1.0}
	if !JSONEqual(left, right) {
		t.Fatal("key order must not matter")
	}
	if JSONEqual(left, map[string]any{"a": 1.0}) {
		t.Fatal("different key counts differ")
	}
	if JSONEqual([]any{1.0, 2.0}, []any{1.0, 2.0, 3.0}) {
		t.Fatal("different lengths differ")
	}
	if JSONEqual("a", "b") || !JSONEqual(nil, nil) {
		t.Fatal("scalars compare by value")
	}
}

func TestDelayHonoursCancellation(t *testing.T) {
	if err := Delay(0, context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := Delay(10_000, ctx); err == nil {
		t.Fatal("a cancelled context must reject the delay")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation must not wait the full delay")
	}
}
