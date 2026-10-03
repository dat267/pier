package durable

import (
	"encoding/json"
	"errors"
	"testing"
)

// Port of the scheduler's definition fit/migration and inspection.

func registryWithTasks(tasks ...Task) *testRegistry {
	registry := newTestRegistry()
	registry.tasks = tasks
	return registry
}

func TestFitRecord(t *testing.T) {
	same := Task{Definition: TaskDefinition{Name: "x", Version: 1}}
	record := TaskRecord{ID: 1, Kind: "x", Version: 1}
	if fit := FitRecord(record, &same, nil); fit.Reason != "" || fit.Migrates {
		t.Fatalf("fit = %+v", fit)
	}
	if fit := FitRecord(record, nil, nil); fit.Reason != BlockedMissingTask {
		t.Fatalf("fit = %+v", fit)
	}
	older := Task{Definition: TaskDefinition{Name: "x", Version: 0}}
	if fit := FitRecord(record, &older, nil); fit.Reason != BlockedTaskTooOld {
		t.Fatalf("fit = %+v", fit)
	}
	newer := Task{Definition: TaskDefinition{Name: "x", Version: 2}}
	if fit := FitRecord(record, &newer, nil); !fit.Migrates {
		t.Fatalf("fit = %+v", fit)
	}
	// A migration already tried for the same name/version stays failed.
	failed := map[Id]FailedMigration{1: {TaskName: "x", Version: 2, Error: errors.New("boom")}}
	if fit := FitRecord(record, &newer, failed); fit.Reason != BlockedMigrationFailed || fit.Error == nil {
		t.Fatalf("fit = %+v", fit)
	}
}

func TestResolveRecordMigration(t *testing.T) {
	migrating := Task{Definition: TaskDefinition{Name: "x", Version: 2,
		Migrate: func(input, checkpoint json.RawMessage, from int) (json.RawMessage, json.RawMessage, error) {
			if from != 1 {
				t.Fatalf("from = %d", from)
			}
			return json.RawMessage(`{"a":2}`), json.RawMessage(`{"phase":"y"}`), nil
		}}}
	registry := registryWithTasks(migrating)
	record := TaskRecord{ID: 1, Kind: "x", Version: 1,
		Input: json.RawMessage(`{"a":1}`), State: TaskState{Status: TaskRunning, Checkpoint: json.RawMessage(`{"phase":"x"}`)}}
	resolution := ResolveRecord(record, registry, map[Id]FailedMigration{}, nil)
	if resolution.Kind != SchedulerReady || resolution.Record.Version != 2 ||
		string(resolution.Record.Input) != `{"a":2}` || string(resolution.Record.State.Checkpoint) != `{"phase":"y"}` {
		t.Fatalf("resolution = %+v", resolution)
	}
	// A failing migration blocks and is recorded and reported.
	failing := Task{Definition: TaskDefinition{Name: "x", Version: 2,
		Migrate: func(input, checkpoint json.RawMessage, from int) (json.RawMessage, json.RawMessage, error) {
			return nil, nil, errors.New("boom")
		}}}
	failed := map[Id]FailedMigration{}
	reported := []error{}
	resolution = ResolveRecord(record, registryWithTasks(failing), failed, func(err error) { reported = append(reported, err) })
	if resolution.Kind != SchedulerBlocked || resolution.Reason != BlockedMigrationFailed {
		t.Fatalf("resolution = %+v", resolution)
	}
	if entry, present := failed[1]; !present || entry.TaskName != "x" {
		t.Fatalf("failed = %+v", failed)
	}
	if len(reported) != 1 {
		t.Fatalf("reported = %+v", reported)
	}
	// A newer definition without a migration blocks at resolution.
	noMigration := Task{Definition: TaskDefinition{Name: "x", Version: 2}}
	resolution = ResolveRecord(record, registryWithTasks(noMigration), map[Id]FailedMigration{}, nil)
	if resolution.Kind != SchedulerBlocked || resolution.Reason != BlockedMigrationFailed {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestInspectTask(t *testing.T) {
	ready := Task{Definition: TaskDefinition{Name: "x", Version: 1}}
	registry := registryWithTasks(ready)
	record := TaskRecord{ID: 1, Kind: "x", Version: 1, State: TaskState{Status: TaskPending}}
	if inspection := InspectTask(record, registry, nil, nil, nil, nil); inspection.Kind != TaskInspectionReady {
		t.Fatalf("inspection = %+v", inspection)
	}
	if inspection := InspectTask(record, registry, nil, nil, map[Id]bool{1: true}, nil); inspection.Kind != TaskInspectionRunning {
		t.Fatalf("inspection = %+v", inspection)
	}
	completing := record
	completing.State = TaskState{Status: TaskCompleting, Outcome: &TaskOutcome{Status: OutcomeCompleted}}
	if inspection := InspectTask(completing, registry, nil, nil, nil, nil); inspection.Kind != TaskInspectionCompleting {
		t.Fatalf("inspection = %+v", inspection)
	}
	waiting := record
	waiting.State = TaskState{Status: TaskWaiting, On: []Id{3}}
	live := map[Id]TaskRecord{3: {ID: 3, State: TaskState{Status: TaskRunning}}}
	inspection := InspectTask(waiting, registry, nil, live, nil, nil)
	if inspection.Kind != TaskInspectionWaiting || len(inspection.On) != 1 || inspection.On[0] != 3 {
		t.Fatalf("inspection = %+v", inspection)
	}
	// A missing definition is blocked.
	if inspection := InspectTask(record, registryWithTasks(), nil, nil, nil, nil); inspection.Kind != TaskInspectionBlocked ||
		inspection.Reason != BlockedMissingTask {
		t.Fatalf("inspection = %+v", inspection)
	}
	// A newer definition without a migration is blocked as a failed migration.
	newer := Task{Definition: TaskDefinition{Name: "x", Version: 2}}
	inspection = InspectTask(record, registryWithTasks(newer), nil, nil, nil, nil)
	if inspection.Kind != TaskInspectionBlocked || inspection.Reason != BlockedMigrationFailed || inspection.Error == nil {
		t.Fatalf("inspection = %+v", inspection)
	}
}
