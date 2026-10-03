package durable

import (
	"context"
	"encoding/json"
	"testing"
)

// Port of the scheduler's open-time live-task load.

func TestLoadLiveTasks(t *testing.T) {
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
	// A crashed running task, a failFast waiter, and a pending task.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		running, err := storage.Task(ctx, ids[0])
		if err != nil {
			return err
		}
		running.State = TaskState{Status: TaskRunning, Checkpoint: json.RawMessage(`{"phase":"prepare","attempt":1}`)}
		if err := tx.SetTask(running); err != nil {
			return err
		}
		waiting, err := storage.Task(ctx, ids[1])
		if err != nil {
			return err
		}
		waiting.State = TaskState{Status: TaskWaiting, On: []Id{ids[0]}, Policy: JoinFailFast}
		return tx.SetTask(waiting)
	}); err != nil {
		t.Fatal(err)
	}
	mirror := NewSchedulerMirror()
	var runningStaged TaskRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if err := LoadLiveTasks(tx, mirror); err != nil {
			return err
		}
		runningStaged = stagedOf(tx, ids[0])
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(mirror.Live) != 3 {
		t.Fatalf("live = %+v", mirror.Live)
	}
	// The crashed running task is reset to pending.
	if runningStaged.State.Status != TaskPending ||
		string(runningStaged.State.Checkpoint) != `{"phase":"prepare","attempt":1}` {
		t.Fatalf("staged = %+v", runningStaged.State)
	}
	if !mirror.FailFastChecks[ids[1]] {
		t.Fatalf("checks = %+v", mirror.FailFastChecks)
	}
}
