package durable

import (
	"context"
	"encoding/json"
	"testing"
)

// Port of the TaskScheduler reservation commit.

// enableSchedulingWithoutDrain turns scheduling on without kicking the drain
// loop, so a manual Reserve is not raced by it (the run-invocation tests use
// the drain deliberately).
func enableSchedulingWithoutDrain(scheduler *TaskScheduler) {
	scheduler.mu.Lock()
	scheduler.enabled = true
	scheduler.mu.Unlock()
}

func TestTaskSchedulerReserve(t *testing.T) {
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
	scheduler := newTestScheduler(t, session, storage)
	// Scheduling is paused, so nothing reserves.
	if reservations, err := scheduler.Reserve(ctx); err != nil || len(reservations) != 0 {
		t.Fatalf("invocations = %+v, %v", reservations, err)
	}
	enableSchedulingWithoutDrain(scheduler)
	reservations, err := scheduler.Reserve(ctx)
	if err != nil || len(reservations) != 1 || reservations[0].Invocation.Mode != "run" || reservations[0].Invocation.TaskID != taskID {
		t.Fatalf("invocations = %+v, %v", reservations, err)
	}
	if !scheduler.RunningInvocations()[taskID] {
		t.Fatal("the invocation is registered")
	}
	record, err := scheduler.GetTask(ctx, taskID)
	if err != nil || record.State.Status != TaskRunning {
		t.Fatalf("record = %+v, %v", record, err)
	}
	// A task with a live invocation is not reserved again.
	if reservations, err := scheduler.Reserve(ctx); err != nil || len(reservations) != 0 {
		t.Fatalf("invocations = %+v, %v", reservations, err)
	}
}

func TestTaskSchedulerReserveOrphans(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var taskID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		definition := TaskDefinition{Name: "pi.missing", Version: 1,
			Initial: func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{"phase":"x"}`), nil }}
		var err error
		taskID, err = tx.CreateTask(definition, json.RawMessage(`{}`),
			TaskOptions{Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, err := storage.Task(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	record.AbortRequested = true
	if err := session.Commit(ctx, func(tx *Transaction) error { return tx.SetTask(record) }); err != nil {
		t.Fatal(err)
	}
	scheduler := newTestScheduler(t, session, storage)
	scheduler.Resume()
	invocations, err := scheduler.Reserve(ctx)
	if err != nil || len(invocations) != 0 {
		t.Fatalf("invocations = %+v, %v", invocations, err)
	}
	scheduled, err := scheduler.GetTask(ctx, taskID)
	if err != nil || scheduled.State.Status != TaskTerminal || scheduled.State.Outcome == nil ||
		scheduled.State.Outcome.Status != OutcomeOrphaned {
		t.Fatalf("record = %+v, %v", scheduled, err)
	}
}
