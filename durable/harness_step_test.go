package durable

import (
	"context"
	"encoding/json"
	"testing"
)

// Port of the scheduler's phase decision, wait validation and idle check.

func TestDecidePhaseStops(t *testing.T) {
	old := Task{Definition: TaskDefinition{Name: "x", Version: 1}}
	record := TaskRecord{ID: 7, Kind: "x", Version: 1,
		State: TaskState{Status: TaskRunning, Checkpoint: json.RawMessage(`{"phase":"a"}`)}}
	// An abort mark ends the invocation without a fault.
	marked := record
	marked.AbortRequested = true
	if decision, err := DecidePhase(nil, marked, nil, &PhaseState{Task: &old}, nil); err != nil || decision.Continue || decision.Fault != nil {
		t.Fatalf("decision = %+v, %v", decision, err)
	}
	// The first phase always continues.
	if decision, _ := DecidePhase(nil, record, nil, &PhaseState{Task: &old}, nil); !decision.Continue {
		t.Fatalf("decision = %+v", decision)
	}
	// A phase failure faults.
	failure := PhaseResult{Checkpoint: json.RawMessage(`{"phase":"a"}`), HasFailure: true, Failure: errSentinel}
	if decision, _ := DecidePhase(nil, record, &failure, &PhaseState{Task: &old}, nil); decision.Fault != errSentinel {
		t.Fatalf("decision = %+v", decision)
	}
	// No durable progress faults with the phase's name.
	same := PhaseResult{Checkpoint: json.RawMessage(`{"phase":"a"}`)}
	decision, err := DecidePhase(nil, record, &same, &PhaseState{Task: &old}, nil)
	if err != nil || decision.Fault == nil ||
		decision.Fault.Error() != "Task x phase a returned without durable progress" {
		t.Fatalf("decision = %+v, %v", decision, err)
	}
	// A changed checkpoint continues.
	changed := PhaseResult{Checkpoint: json.RawMessage(`{"phase":"b"}`)}
	if decision, _ := DecidePhase(nil, record, &changed, &PhaseState{Task: &old}, nil); !decision.Continue {
		t.Fatalf("decision = %+v", decision)
	}
}

var errSentinel = &sentinelError{}

type sentinelError struct{}

func (*sentinelError) Error() string { return "sentinel" }

func TestDecidePhaseHandsOver(t *testing.T) {
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
	stored, err := storage.Task(ctx, taskID)
	if err != nil || stored == nil {
		t.Fatalf("task = %+v, %v", stored, err)
	}
	record := *stored
	record.State = TaskState{Status: TaskRunning, Checkpoint: json.RawMessage(`{"phase":"a"}`)}
	old := Task{Definition: TaskDefinition{Name: RunTaskKind, Version: 1}}
	newer := Task{Definition: TaskDefinition{Name: RunTaskKind, Version: 2,
		Migrate: func(input, checkpoint json.RawMessage, from int) (json.RawMessage, json.RawMessage, error) {
			return input, checkpoint, nil
		}}}
	registry := registryWithTasks(newer)
	previous := PhaseResult{Checkpoint: json.RawMessage(`{"phase":"b"}`)}
	state := &PhaseState{Task: &old, Snapshot: registry, Refresh: func() RegistrySnapshot { return registry }}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		decision, err := DecidePhase(tx, record, &previous, state, nil)
		if err != nil || decision.Continue {
			t.Fatalf("decision = %+v, %v", decision, err)
		}
		staged := tx.StagedTasks()
		found := false
		for _, candidate := range staged {
			if candidate.ID == taskID {
				if candidate.State.Status != TaskPending {
					t.Fatalf("candidate = %+v", candidate.State)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("staged = %+v", staged)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDecidePhaseReportsIncompatible(t *testing.T) {
	old := Task{Definition: TaskDefinition{Name: "x", Version: 1}}
	incompatible := Task{Definition: TaskDefinition{Name: "x", Version: 2}}
	registry := registryWithTasks(incompatible)
	record := TaskRecord{ID: 7, Kind: "x", Version: 1,
		State: TaskState{Status: TaskRunning, Checkpoint: json.RawMessage(`{"phase":"a"}`)}}
	previous := PhaseResult{Checkpoint: json.RawMessage(`{"phase":"b"}`)}
	reported := []error{}
	state := &PhaseState{Task: &old, Snapshot: registry, Refresh: func() RegistrySnapshot { return registry }}
	if decision, err := DecidePhase(nil, record, &previous, state, func(err error) { reported = append(reported, err) }); err != nil || !decision.Continue {
		t.Fatalf("decision = %+v, %v", decision, err)
	}
	if len(reported) != 1 {
		t.Fatalf("reported = %+v", reported)
	}
	var mismatch *DefinitionMismatchError
	if !errorsAs(reported[0], &mismatch) || mismatch.Cause != "incompatible_task" {
		t.Fatalf("reported = %+v", reported[0])
	}
	// The same replacement is reported once.
	if _, _ = DecidePhase(nil, record, &previous, state, func(err error) { reported = append(reported, err) }); len(reported) != 1 {
		t.Fatalf("reported = %+v", reported)
	}
	// A missing definition reports the missing cause.
	missingRegistry := registryWithTasks()
	state = &PhaseState{Task: &old, Snapshot: missingRegistry, Refresh: func() RegistrySnapshot { return missingRegistry }}
	if _, _ = DecidePhase(nil, record, &previous, state, func(err error) { reported = append(reported, err) }); len(reported) != 2 {
		t.Fatalf("reported = %+v", reported)
	}
}

func errorsAs(err error, target **DefinitionMismatchError) bool {
	mismatch, ok := err.(*DefinitionMismatchError)
	if ok {
		*target = mismatch
	}
	return ok
}

func TestValidateWait(t *testing.T) {
	current := TaskRecord{ID: 1, State: TaskState{Status: TaskRunning}}
	member := func(id Id) (*TaskRecord, error) {
		records := map[Id]*TaskRecord{
			2: {ID: 2},
			3: {ID: 3, Owner: idPointerOf(1)},
		}
		return records[id], nil
	}
	if err := ValidateWait("abort", current, []Id{2}, JoinAllSettled, nil, member); err == nil {
		t.Fatal("an abort handler cannot wait")
	}
	if err := ValidateWait("run", current, []Id{1}, JoinAllSettled, nil, member); err == nil {
		t.Fatal("waiting on itself must fail")
	}
	if err := ValidateWait("run", current, []Id{9}, JoinAllSettled, map[Id]bool{9: true}, member); err == nil {
		t.Fatal("waiting on an owner must fail")
	}
	if err := ValidateWait("run", current, []Id{4}, JoinAllSettled, nil, member); err == nil {
		t.Fatal("waiting on a missing task must fail")
	}
	if err := ValidateWait("run", current, []Id{2}, JoinFailFast, nil, member); err == nil {
		t.Fatal("failFast on an unowned task must fail")
	}
	if err := ValidateWait("run", current, []Id{3}, JoinFailFast, nil, member); err != nil {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateWait("run", current, []Id{2}, JoinAllSettled, nil, member); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestIdle(t *testing.T) {
	record := traversalRecord(3, nil, false, TaskRunning)
	graph := SchedulerGraph{Edges: map[Id]*Id{1: nil}}
	// A live non-background task inside the roots scope is not idle.
	if Idle(nil, []TaskRecord{record}, graph) {
		t.Fatal("a live task is not idle")
	}
	// Out of scope, it is idle.
	if !Idle(idPointerOf(2), []TaskRecord{record}, graph) {
		t.Fatal("an out-of-scope task is idle")
	}
	// A background task never counts.
	record.Background = true
	if !Idle(nil, []TaskRecord{record}, graph) {
		t.Fatal("a background task does not count")
	}
	// An unloaded edge counts as inside.
	if Idle(nil, []TaskRecord{traversalRecord(3, nil, false, TaskRunning)}, SchedulerGraph{}) {
		t.Fatal("an unknown chain is not idle")
	}
}
