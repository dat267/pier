package durable

import (
	"context"
	"testing"
	"time"
)

// Port of the TaskScheduler core.

func newTestScheduler(t *testing.T, session *Session, storage Storage) *TaskScheduler {
	t.Helper()
	scheduler := NewTaskScheduler(TaskSchedulerOptions{
		Session: session, Storage: storage, Registry: NewRegistry(),
		Context: context.Background(),
	})
	if err := scheduler.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	return scheduler
}

func TestTaskSchedulerOpenAndWaitForTask(t *testing.T) {
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
	if record, err := scheduler.GetTask(ctx, taskID); err != nil || record == nil || record.ID != taskID {
		t.Fatalf("getTask = %+v, %v", record, err)
	}
	settled := make(chan SettledTask, 1)
	errs := make(chan error, 1)
	go func() {
		record, err := scheduler.WaitForTask(ctx, taskID)
		if err != nil {
			errs <- err
			return
		}
		settled <- record
	}()
	// Wait for the registration on the line, then settle the task.
	deadline := time.Now().Add(2 * time.Second)
	for len(scheduler.taskWaiters.Keys()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	record, err := storage.Task(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	record.State = TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}}
	if err := session.Commit(ctx, func(tx *Transaction) error { return tx.SetTask(record) }); err != nil {
		t.Fatal(err)
	}
	select {
	case resolved := <-settled:
		if resolved.State.Status != TaskTerminal {
			t.Fatalf("resolved = %+v", resolved)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not resolve")
	}
}

func TestTaskSchedulerSealRejectsWaiters(t *testing.T) {
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
	errs := make(chan error, 1)
	go func() {
		_, err := scheduler.WaitForTask(ctx, taskID)
		errs <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(scheduler.taskWaiters.Keys()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	scheduler.Seal()
	select {
	case err := <-errs:
		if err == nil || err.Error() != "Harness is closed" {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("seal did not reject the waiter")
	}
	if !scheduler.Closing() {
		t.Fatal("the scheduler is closing")
	}
}

func TestTaskSchedulerInspect(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := CreateGeneration(tx, RootConversationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	scheduler := newTestScheduler(t, session, storage)
	registry := registryWithTasks(BuiltinTaskDefinitions()...)
	if inspection := scheduler.Inspect(registry); inspection.Scheduling != SchedulingPaused || len(inspection.Tasks) != 1 {
		t.Fatalf("inspection = %+v", inspection)
	}
	scheduler.Resume()
	if inspection := scheduler.Inspect(registry); inspection.Scheduling != SchedulingRunning {
		t.Fatalf("scheduling = %q", inspection.Scheduling)
	}
	if !scheduler.Enabled() {
		t.Fatal("the scheduler is enabled")
	}
}
