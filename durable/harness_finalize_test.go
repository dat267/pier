package durable

import (
	"context"
	"encoding/json"
	"testing"
)

// Port of the scheduler's finalize and memo semantics.

func completing(t *testing.T, record TaskRecord, outcome *TaskOutcome) TaskRecord {
	t.Helper()
	record.State = TaskState{Status: TaskCompleting, Outcome: outcome}
	return record
}

func TestFinalizeCompletingCascades(t *testing.T) {
	session, _, parent, child := schedulerTasks(t)
	ctx := context.Background()
	completed := &TaskOutcome{Status: OutcomeCompleted}
	parent = completing(t, parent, completed)
	child = completing(t, child, completed)
	var parentStaged, childStaged TaskRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := FinalizeCompleting(tx, []TaskRecord{parent, child}, nil); err != nil {
			return err
		}
		parentStaged = stagedOf(tx, parent.ID)
		childStaged = stagedOf(tx, child.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if childStaged.State.Status != TaskTerminal || parentStaged.State.Status != TaskTerminal {
		t.Fatalf("parent = %+v, child = %+v", parentStaged.State, childStaged.State)
	}
}

func TestFinalizeCompletingHoldsForLiveWork(t *testing.T) {
	session, _, parent, child := schedulerTasks(t)
	ctx := context.Background()
	completed := &TaskOutcome{Status: OutcomeCompleted}
	parent = completing(t, parent, completed)
	// The child is still running, so the parent has live owned work.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := FinalizeCompleting(tx, []TaskRecord{parent, child}, nil); err != nil {
			return err
		}
		if staged := stagedOf(tx, parent.ID); staged.ID != 0 {
			t.Fatalf("parent must not be finalized: %+v", staged.State)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeCompletingSettlesFaulted(t *testing.T) {
	session, _, parent, child := schedulerTasks(t)
	ctx := context.Background()
	faulted := &TaskOutcome{Status: OutcomeFaulted, Error: &StoredError{Message: "boom"}}
	parent = completing(t, parent, &TaskOutcome{Status: OutcomeCompleted})
	child = completing(t, child, faulted)
	settled := []Id{}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return FinalizeCompleting(tx, []TaskRecord{parent, child}, func(tx *Transaction, record TaskRecord, outcome *TaskOutcome) error {
			settled = append(settled, record.ID)
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	if len(settled) != 1 || settled[0] != child.ID {
		t.Fatalf("settled = %+v", settled)
	}
}

func TestMemoSetFirstWriterWins(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var taskID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		taskID, err = CreateGeneration(tx, RootConversationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, err := storage.Task(ctx, taskID)
	if err != nil || record == nil {
		t.Fatalf("task = %+v, %v", record, err)
	}
	var winner json.RawMessage
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		winner, err = MemoSet(tx, *record, "note", json.RawMessage(`1`))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if string(winner) != "1" {
		t.Fatalf("winner = %s", winner)
	}
	// The recorded memo wins over a later candidate.
	updated, err := storage.Task(ctx, taskID)
	if err != nil || updated == nil {
		t.Fatalf("task = %+v, %v", updated, err)
	}
	if value, present := MemoOf(updated, "note"); !present || string(value) != "1" {
		t.Fatalf("memo = %s, %v", value, present)
	}
	var later json.RawMessage
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		later, err = MemoSet(tx, *updated, "note", json.RawMessage(`2`))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if string(later) != "1" {
		t.Fatalf("later = %s", later)
	}
	final, _ := storage.Task(ctx, taskID)
	if value, _ := MemoOf(final, "note"); string(value) != "1" {
		t.Fatalf("memo = %s", value)
	}
}
