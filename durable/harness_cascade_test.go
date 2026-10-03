package durable

import (
	"testing"
)

// Port of the scheduler's cancellation-mark derivation and failFast checks.

func TestDeriveCancellationMarks(t *testing.T) {
	owner := idPointerOf(2)
	intent := traversalRecord(2, nil, false, TaskRunning)
	intent.AbortRequested = true
	child := traversalRecord(3, owner, false, TaskRunning)
	background := traversalRecord(4, owner, true, TaskRunning)
	marked := traversalRecord(5, owner, false, TaskRunning)
	marked.AbortRequested = true
	graph := SchedulerGraph{
		Live:  map[Id]TaskRecord{2: intent, 3: child, 4: background, 5: marked},
		Edges: map[Id]*Id{1: nil},
	}
	marks := DeriveCancellationMarks([]TaskRecord{child, background, marked}, graph)
	if len(marks) != 1 || marks[0] != 3 {
		t.Fatalf("marks = %+v", marks)
	}
	// Without intent above, nothing is marked.
	graph.Live[2] = traversalRecord(2, nil, false, TaskRunning)
	if marks := DeriveCancellationMarks([]TaskRecord{child}, graph); len(marks) != 0 {
		t.Fatalf("marks = %+v", marks)
	}
}

func TestAnyFailed(t *testing.T) {
	failed := traversalRecord(1, nil, false, TaskCompleting)
	failed.State.Outcome = &TaskOutcome{Status: OutcomeFailed}
	live := map[Id]TaskRecord{1: failed}
	if ok, err := AnyFailed([]Id{1}, live, nil); err != nil || !ok {
		t.Fatalf("anyFailed = %v, %v", ok, err)
	}
	// A task outside the live view is loaded.
	loaded := failed
	ok, err := AnyFailed([]Id{2}, map[Id]TaskRecord{}, func(id Id) (*TaskRecord, error) { return &loaded, nil })
	if err != nil || !ok {
		t.Fatalf("anyFailed = %v, %v", ok, err)
	}
	if ok, err := AnyFailed([]Id{3}, map[Id]TaskRecord{}, func(id Id) (*TaskRecord, error) { return nil, nil }); err != nil || ok {
		t.Fatalf("anyFailed = %v, %v", ok, err)
	}
}

func TestFailFastMarks(t *testing.T) {
	failed := traversalRecord(1, nil, false, TaskCompleting)
	failed.State.Outcome = &TaskOutcome{Status: OutcomeFailed}
	running := traversalRecord(2, nil, false, TaskRunning)
	live := map[Id]TaskRecord{1: failed, 2: running}
	waiter := TaskRecord{State: TaskState{Status: TaskWaiting, On: []Id{1, 2}}}
	marks, err := FailFastMarks(waiter, live, func(ids []Id) (bool, error) { return true, nil })
	if err != nil || len(marks) != 1 || marks[0] != 2 {
		t.Fatalf("marks = %+v, %v", marks, err)
	}
	// When nothing failed there are no marks.
	marks, err = FailFastMarks(waiter, live, func(ids []Id) (bool, error) { return false, nil })
	if err != nil || len(marks) != 0 {
		t.Fatalf("marks = %+v, %v", marks, err)
	}
	// A non-waiting task has nothing to mark.
	runningWaiter := TaskRecord{State: TaskState{Status: TaskRunning, On: []Id{1, 2}}}
	if marks, _ := FailFastMarks(runningWaiter, live, func(ids []Id) (bool, error) { return true, nil }); len(marks) != 0 {
		t.Fatalf("marks = %+v", marks)
	}
}

func TestCancelledScopes(t *testing.T) {
	owner := idPointerOf(5)
	intent := traversalRecord(5, nil, false, TaskRunning)
	intent.AbortRequested = true
	graph := SchedulerGraph{
		Live:  map[Id]TaskRecord{5: intent},
		Edges: map[Id]*Id{1: nil, 2: owner},
	}
	cancelled := CancelledScopes([]Id{1, 2}, graph)
	if len(cancelled) != 1 || cancelled[0] != 2 {
		t.Fatalf("cancelled = %+v", cancelled)
	}
}
