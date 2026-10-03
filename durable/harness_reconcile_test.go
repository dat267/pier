package durable

import (
	"context"
	"testing"
)

// Port of the scheduler's reconcile pass.

func TestReconcileTasksMarksBelowIntent(t *testing.T) {
	session, _, parent, child := schedulerTasks(t)
	ctx := context.Background()
	marked := parent
	marked.AbortRequested = true
	graph := SchedulerGraph{Live: RecordsByID([]TaskRecord{marked, child}), Edges: map[Id]*Id{1: nil}}
	var childStaged TaskRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := ReconcileTasks(tx, []TaskRecord{marked, child}, graph, nil, nil, nil,
			func(tx *Transaction, id Id) error { return nil }, nil); err != nil {
			return err
		}
		childStaged = stagedOf(tx, child.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !childStaged.AbortRequested {
		t.Fatalf("child = %+v", childStaged)
	}
}

func TestReconcileTasksFinalizesAndWithdraws(t *testing.T) {
	session, _, parent, child := schedulerTasks(t)
	ctx := context.Background()
	free := child
	free.State = TaskState{Status: TaskCompleting, Outcome: &TaskOutcome{Status: OutcomeCompleted}}
	// A queued conversation below an intentful owner withdraws its inputs.
	owner := idPointerOf(parent.ID)
	intent := parent
	intent.AbortRequested = true
	graph := SchedulerGraph{
		Live:  RecordsByID([]TaskRecord{intent, free}),
		Edges: map[Id]*Id{1: nil, 2: owner},
	}
	withdrawn := []Id{}
	var childStaged TaskRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := ReconcileTasks(tx, []TaskRecord{intent, free}, graph, []Id{1, 2}, nil, nil,
			func(tx *Transaction, id Id) error { withdrawn = append(withdrawn, id); return nil }, nil); err != nil {
			return err
		}
		childStaged = stagedOf(tx, child.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if childStaged.State.Status != TaskTerminal {
		t.Fatalf("child = %+v", childStaged.State)
	}
	if len(withdrawn) != 1 || withdrawn[0] != 2 {
		t.Fatalf("withdrawn = %+v", withdrawn)
	}
}

func TestReconcileTasksFailFast(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var ids []Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		for index := 0; index < 3; index++ {
			id, err := CreateGeneration(tx, RootConversationID)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	load := func(id Id) (*TaskRecord, error) { return nil, nil }
	failed, err := storage.Task(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	waiter, err := storage.Task(ctx, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	running, err := storage.Task(ctx, ids[2])
	if err != nil {
		t.Fatal(err)
	}
	failed.State = TaskState{Status: TaskCompleting, Outcome: &TaskOutcome{Status: OutcomeFailed}}
	waiter.State = TaskState{Status: TaskWaiting, On: []Id{ids[0], ids[2]}}
	running.State = TaskState{Status: TaskRunning}
	graph := SchedulerGraph{
		Live:  RecordsByID([]TaskRecord{*failed, *waiter, *running}),
		Edges: map[Id]*Id{1: nil},
	}
	var runningStaged, failedStaged TaskRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := ReconcileTasks(tx, []TaskRecord{*failed, *waiter, *running}, graph, nil, []Id{waiter.ID}, load,
			func(tx *Transaction, id Id) error { return nil }, nil); err != nil {
			return err
		}
		runningStaged = stagedOf(tx, running.ID)
		failedStaged = stagedOf(tx, failed.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !runningStaged.AbortRequested {
		t.Fatalf("running = %+v", runningStaged)
	}
	// The failed member keeps its own outcome and is finalized, not marked.
	if failedStaged.AbortRequested || failedStaged.State.Status != TaskTerminal {
		t.Fatalf("failed = %+v", failedStaged)
	}
}
