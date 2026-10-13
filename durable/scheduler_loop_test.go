package durable

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/chord"
)

// Port of the scheduler drain loop and abort methods.

func twoPhaseDefinition() TaskDefinition {
	return TaskDefinition{Name: "test.two", Version: 1,
		Initial: func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{"phase":"one"}`), nil },
		Phases: map[string]PhaseHandler{
			"one": func(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
				return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
					return &NextTaskState{Status: TaskRunning, Checkpoint: json.RawMessage(`{"phase":"two"}`)}, nil
				}, ctx)
			},
			"two": func(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
				return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
					return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}}, nil
				}, ctx)
			},
		},
	}
}

func TestTaskSchedulerDrainRunsToTerminal(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	definition := twoPhaseDefinition()
	var taskID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		taskID, err = tx.CreateTask(definition, json.RawMessage(`{}`),
			TaskOptions{Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Install(Extension{Name: "test", Tasks: []Task{{Definition: definition}}}); err != nil {
		t.Fatal(err)
	}
	scheduler := NewTaskScheduler(TaskSchedulerOptions{
		Session: session, Storage: storage, Registry: registry, Context: context.Background(),
	})
	if err := scheduler.Open(ctx); err != nil {
		t.Fatal(err)
	}
	// Resume kicks the drain, which reserves and runs the ready task.
	scheduler.Resume()
	settled, err := scheduler.WaitForTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.State.Status != TaskTerminal || settled.State.Outcome == nil ||
		settled.State.Outcome.Status != OutcomeCompleted {
		t.Fatalf("settled = %+v", settled)
	}
}

func TestTaskSchedulerRejectsWaitThatClosesCycle(t *testing.T) {
	ctx := context.Background()
	session, storage := newRootSession(t)
	var taskA, taskB Id
	makeWaitTask := func(name string, target func() Id) TaskDefinition {
		return TaskDefinition{Name: name, Version: 1,
			Initial: func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{"phase":"wait"}`), nil },
			Phases: map[string]PhaseHandler{
				"wait": func(_ RunningTask, runtime TaskRuntime, phaseCtx chord.Context) error {
					return runtime.Commit(func(*Transaction, RunningTask) (*NextTaskState, error) {
						return &NextTaskState{Status: TaskWaiting, On: []Id{target()}, Policy: JoinAllSettled}, nil
					}, phaseCtx)
				},
			},
		}
	}
	waitA := makeWaitTask("test.wait-a", func() Id { return taskB })
	waitB := makeWaitTask("test.wait-b", func() Id { return taskA })
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		taskA, err = tx.CreateTask(waitA, json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID),
		})
		if err != nil {
			return err
		}
		taskB, err = tx.CreateTask(waitB, json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Install(Extension{Name: "test", Tasks: []Task{{Definition: waitA}, {Definition: waitB}}}); err != nil {
		t.Fatal(err)
	}
	scheduler := NewTaskScheduler(TaskSchedulerOptions{Session: session, Storage: storage, Registry: registry, Context: ctx})
	defer scheduler.Seal()
	if err := scheduler.Open(ctx); err != nil {
		t.Fatal(err)
	}
	scheduler.Resume()
	deadline := time.Now().Add(time.Second)
	var cycleError string
	for time.Now().Before(deadline) {
		for _, id := range []Id{taskA, taskB} {
			record, err := scheduler.GetTask(ctx, id)
			if err != nil || record == nil || record.State.Outcome == nil || record.State.Outcome.Error == nil {
				continue
			}
			if strings.Contains(record.State.Outcome.Error.Message, "wait cycle") {
				cycleError = record.State.Outcome.Error.Message
				break
			}
		}
		if cycleError != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if cycleError == "" {
		a, _ := scheduler.GetTask(ctx, taskA)
		b, _ := scheduler.GetTask(ctx, taskB)
		t.Fatalf("wait cycle was not rejected: A=%+v B=%+v", a, b)
	}
}

func TestTaskSchedulerAbortMarks(t *testing.T) {
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
	result, err := scheduler.Abort(taskID, ctx)
	if err != nil || result != "marked" {
		t.Fatalf("result = %q, %v", result, err)
	}
	record, err := scheduler.GetTask(ctx, taskID)
	if err != nil || !record.AbortRequested {
		t.Fatalf("record = %+v, %v", record, err)
	}
	// A terminal task reports terminal.
	record.State = TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}}
	if err := session.Commit(ctx, func(tx *Transaction) error { return tx.SetTask(record) }); err != nil {
		t.Fatal(err)
	}
	if result, err := scheduler.Abort(taskID, ctx); err != nil || result != "terminal" {
		t.Fatalf("result = %q, %v", result, err)
	}
}

func TestTaskSchedulerWaitForIdleEmpty(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	scheduler := newTestScheduler(t, session, storage)
	if err := scheduler.WaitForIdle(idPointerOf(RootConversationID), ctx); err != nil {
		t.Fatalf("err = %v", err)
	}
}
