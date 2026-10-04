package durable

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dat267/pier/chord"
)

// Port of the scheduler's phase loop.

func TestTaskSchedulerRunInvocationPhases(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	definition := TaskDefinition{Name: "test.two", Version: 1,
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
	// The drain loop owns reservation and invocation; waiting for the task
	// observes the phase loop without racing a manual Reserve.
	scheduler.Resume()
	settled, err := scheduler.WaitForTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.State.Status != TaskTerminal || settled.State.Outcome == nil ||
		settled.State.Outcome.Status != OutcomeCompleted {
		t.Fatalf("settled = %+v", settled)
	}
	// The invocation is freed just after its done signal, so poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for scheduler.RunningInvocations()[taskID] && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if scheduler.RunningInvocations()[taskID] {
		t.Fatal("the invocation must be freed")
	}
}

func TestTaskSchedulerRunInvocationFaultsWithoutProgress(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	definition := TaskDefinition{Name: "test.stuck", Version: 1,
		Initial: func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{"phase":"one"}`), nil },
		Phases: map[string]PhaseHandler{
			// Returns without committing durable progress.
			"one": func(task RunningTask, runtime TaskRuntime, ctx chord.Context) error { return nil },
		},
	}
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
	// The drain loop owns reservation and invocation; waiting for the task
	// observes the fault without racing a manual Reserve.
	scheduler.Resume()
	settled, err := scheduler.WaitForTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.State.Status != TaskTerminal || settled.State.Outcome == nil ||
		settled.State.Outcome.Status != OutcomeFaulted {
		t.Fatalf("settled = %+v", settled)
	}
}
