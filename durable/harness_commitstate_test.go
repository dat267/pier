package durable

import (
	"context"
	"testing"
)

// Port of the scheduler's terminal-state hold and overlay live records.

func TestLiveRecordsWithOverlay(t *testing.T) {
	live := []TaskRecord{
		traversalRecord(1, nil, false, TaskRunning),
		traversalRecord(2, nil, false, TaskRunning),
	}
	terminated := traversalRecord(1, nil, false, TaskTerminal)
	added := traversalRecord(3, nil, false, TaskRunning)
	overlay := SchedulerOverlay{Tasks: map[Id]TaskRecord{1: terminated, 3: added}, Edges: map[Id]*Id{}}
	records := LiveRecordsWithOverlay(live, overlay)
	if len(records) != 2 || records[0].ID != 2 || records[1].ID != 3 {
		t.Fatalf("records = %+v", records)
	}
}

func TestOwnedTaskIDs(t *testing.T) {
	owner := idPointerOf(2)
	records := []TaskRecord{
		traversalRecord(3, owner, false, TaskRunning),
		traversalRecord(2, nil, false, TaskRunning),
	}
	graph := SchedulerGraph{Live: RecordsByID(records), Edges: map[Id]*Id{1: nil}}
	owned := OwnedTaskIDs(records, graph)
	if !owned[2] || owned[3] {
		t.Fatalf("owned = %+v", owned)
	}
}

func schedulerTasks(t *testing.T) (*Session, Storage, TaskRecord, TaskRecord) {
	t.Helper()
	session, storage := newRootSession(t)
	ctx := context.Background()
	var parentID, childID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		parentID, err = CreateGeneration(tx, RootConversationID)
		if err != nil {
			return err
		}
		childID, err = CreateToolTask(tx, parentID, 1, "c1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	parent, err := storage.Task(ctx, parentID)
	if err != nil || parent == nil {
		t.Fatalf("parent = %+v, %v", parent, err)
	}
	child, err := storage.Task(ctx, childID)
	if err != nil || child == nil {
		t.Fatalf("child = %+v, %v", child, err)
	}
	parent.State = TaskState{Status: TaskRunning, Checkpoint: []byte(`{"phase":"prepare","attempt":1}`)}
	child.State = TaskState{Status: TaskRunning, Checkpoint: []byte(`{"phase":"call"}`)}
	return session, storage, *parent, *child
}

func TestCommitTaskStateHoldsCompleting(t *testing.T) {
	session, _, parent, child := schedulerTasks(t)
	ctx := context.Background()
	outcome := &TaskOutcome{Status: OutcomeCompleted}
	// The parent has live owned work, so its terminal state holds as completing.
	var parentStaged TaskRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := CommitTaskState(tx, []TaskRecord{parent, child}, parent,
			NextTaskState{Status: TaskTerminal, Outcome: outcome}, nil); err != nil {
			return err
		}
		parentStaged = stagedOf(tx, parent.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if parentStaged.State.Status != TaskCompleting || parentStaged.State.Outcome == nil {
		t.Fatalf("staged = %+v", parentStaged.State)
	}
	// The child owns nothing, so it becomes terminal directly.
	var childStaged TaskRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := CommitTaskState(tx, []TaskRecord{parent, child}, child,
			NextTaskState{Status: TaskTerminal, Outcome: outcome}, nil); err != nil {
			return err
		}
		childStaged = stagedOf(tx, child.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if childStaged.State.Status != TaskTerminal {
		t.Fatalf("staged = %+v", childStaged.State)
	}
}

func TestCommitTaskStateValidatesWait(t *testing.T) {
	session, _, _, child := schedulerTasks(t)
	ctx := context.Background()
	called := false
	err := session.Commit(ctx, func(tx *Transaction) error {
		return CommitTaskState(tx, []TaskRecord{child}, child,
			NextTaskState{Status: TaskWaiting, On: []Id{5}, Policy: JoinAllSettled},
			func(on []Id, policy string) error { called = true; return errSentinel })
	})
	if err != errSentinel || !called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestTerminateTask(t *testing.T) {
	session, _, parent, child := schedulerTasks(t)
	ctx := context.Background()
	outcome := &TaskOutcome{Status: OutcomeFaulted, Error: &StoredError{Message: "boom"}}
	// Live owned work holds the parent completing and skips the cleanup.
	settled := false
	var parentStaged TaskRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := TerminateTask(tx, []TaskRecord{parent, child}, parent, outcome,
			func(tx *Transaction, record TaskRecord, outcome *TaskOutcome) error { settled = true; return nil }); err != nil {
			return err
		}
		parentStaged = stagedOf(tx, parent.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if parentStaged.State.Status != TaskCompleting || settled {
		t.Fatalf("staged = %+v, settled = %v", parentStaged.State, settled)
	}
	// The child becomes terminal and the cleanup runs.
	var childStaged TaskRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := TerminateTask(tx, []TaskRecord{parent, child}, child, outcome,
			func(tx *Transaction, record TaskRecord, outcome *TaskOutcome) error { settled = true; return nil }); err != nil {
			return err
		}
		childStaged = stagedOf(tx, child.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if childStaged.State.Status != TaskTerminal || !settled {
		t.Fatalf("staged = %+v, settled = %v", childStaged.State, settled)
	}
}

// stagedOf is the staged candidate for one task id inside a commit.
func stagedOf(tx *Transaction, id Id) TaskRecord {
	for _, candidate := range tx.StagedTasks() {
		if candidate.ID == id {
			return *candidate
		}
	}
	return TaskRecord{}
}
