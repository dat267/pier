package durable

import (
	"encoding/json"
	"testing"
)

// Port of the scheduler's reservation planner.

func TestPlanReservationsWaitsForOwnedWork(t *testing.T) {
	owner := idPointerOf(2)
	parent := traversalRecord(2, nil, false, TaskPending)
	parent.Kind = RunTaskKind
	parent.AbortRequested = true
	child := traversalRecord(3, owner, false, TaskPending)
	child.Kind = ToolTaskKind
	registry := registryWithTasks(BuiltinTaskDefinitions()...)
	graph := SchedulerGraph{Live: RecordsByID([]TaskRecord{parent, child}), Edges: map[Id]*Id{1: nil}}
	plans, orphans := PlanReservations([]TaskRecord{parent, child}, graph, registry, nil, map[Id]FailedMigration{}, nil)
	if len(orphans) != 0 {
		t.Fatalf("orphans = %+v", orphans)
	}
	if len(plans) != 1 || plans[0].TaskID != child.ID || plans[0].Mode != "run" || plans[0].SetRunning == nil {
		t.Fatalf("plans = %+v", plans)
	}
	// A running child is not re-staged.
	childRunning := child
	childRunning.State.Status = TaskRunning
	plans, _ = PlanReservations([]TaskRecord{parent, childRunning}, graph, registry, nil, map[Id]FailedMigration{}, nil)
	if len(plans) != 1 || plans[0].SetRunning != nil {
		t.Fatalf("plans = %+v", plans)
	}
}

func TestPlanReservationsOrphansBlockedAbort(t *testing.T) {
	marked := traversalRecord(4, nil, false, TaskPending)
	marked.Kind = "pi.missing"
	marked.AbortRequested = true
	unmarked := traversalRecord(5, nil, false, TaskPending)
	unmarked.Kind = "pi.missing"
	registry := registryWithTasks(BuiltinTaskDefinitions()...)
	graph := SchedulerGraph{Live: RecordsByID([]TaskRecord{marked, unmarked}), Edges: map[Id]*Id{1: nil}}
	plans, orphans := PlanReservations([]TaskRecord{marked, unmarked}, graph, registry, nil, map[Id]FailedMigration{}, nil)
	if len(plans) != 0 {
		t.Fatalf("plans = %+v", plans)
	}
	if len(orphans) != 1 || orphans[0].Record.ID != marked.ID || orphans[0].Reason != BlockedMissingTask {
		t.Fatalf("orphans = %+v", orphans)
	}
}

func TestPlanReservationsSkipsBusy(t *testing.T) {
	record := traversalRecord(6, nil, false, TaskPending)
	record.Kind = RunTaskKind
	registry := registryWithTasks(BuiltinTaskDefinitions()...)
	graph := SchedulerGraph{Live: RecordsByID([]TaskRecord{record}), Edges: map[Id]*Id{1: nil}}
	if plans, _ := PlanReservations([]TaskRecord{record}, graph, registry, map[Id]bool{record.ID: true}, map[Id]FailedMigration{}, nil); len(plans) != 0 {
		t.Fatalf("plans = %+v", plans)
	}
	completing := record
	completing.State.Status = TaskCompleting
	completing.State.Outcome = &TaskOutcome{Status: OutcomeCompleted}
	if plans, _ := PlanReservations([]TaskRecord{completing}, graph, registry, nil, map[Id]FailedMigration{}, nil); len(plans) != 0 {
		t.Fatalf("plans = %+v", plans)
	}
}

func TestPlanReservationsMigrates(t *testing.T) {
	migrating := Task{Definition: TaskDefinition{Name: RunTaskKind, Version: 2,
		Migrate: func(input, checkpoint json.RawMessage, from int) (json.RawMessage, json.RawMessage, error) {
			return input, checkpoint, nil
		}}}
	record := traversalRecord(7, nil, false, TaskPending)
	record.Kind = RunTaskKind
	record.Version = 1
	registry := registryWithTasks(migrating)
	graph := SchedulerGraph{Live: RecordsByID([]TaskRecord{record}), Edges: map[Id]*Id{1: nil}}
	plans, _ := PlanReservations([]TaskRecord{record}, graph, registry, nil, map[Id]FailedMigration{}, nil)
	if len(plans) != 1 || plans[0].Record.Version != 2 || plans[0].SetRunning == nil {
		t.Fatalf("plans = %+v", plans)
	}
}
