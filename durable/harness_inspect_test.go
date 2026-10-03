package durable

import (
	"testing"
)

// Port of the scheduler's inspection view.

func TestBuildInspection(t *testing.T) {
	owner := idPointerOf(2)
	parent := traversalRecord(2, nil, false, TaskPending)
	parent.Kind = RunTaskKind
	child := traversalRecord(3, owner, false, TaskPending)
	child.Kind = ToolTaskKind
	records := []TaskRecord{parent, child}
	mirror := NewSchedulerMirror()
	mirror.Live = RecordsByID(records)
	mirror.Edges = map[Id]*Id{1: nil}
	registry := registryWithTasks(BuiltinTasks...)
	inspection := BuildInspection(records, mirror, registry, nil, false, true)
	if inspection.Scheduling != SchedulingRunning || len(inspection.Tasks) != 2 {
		t.Fatalf("inspection = %+v", inspection)
	}
	if inspection.Tasks[0].Kind != TaskInspectionReady || inspection.Tasks[1].Kind != TaskInspectionReady {
		t.Fatalf("tasks = %+v", inspection.Tasks)
	}
	// A running invocation shows as running.
	inspection = BuildInspection(records, mirror, registry, map[Id]bool{child.ID: true}, false, true)
	if inspection.Tasks[1].Kind != TaskInspectionRunning {
		t.Fatalf("tasks = %+v", inspection.Tasks)
	}
	// Closing and paused scheduling states.
	if inspection := BuildInspection(records, mirror, registry, nil, true, true); inspection.Scheduling != SchedulingClosing {
		t.Fatalf("scheduling = %q", inspection.Scheduling)
	}
	if inspection := BuildInspection(records, mirror, registry, nil, false, false); inspection.Scheduling != SchedulingPaused {
		t.Fatalf("scheduling = %q", inspection.Scheduling)
	}
	// A missing definition is blocked.
	unknown := traversalRecord(4, nil, false, TaskPending)
	unknown.Kind = "pi.missing"
	inspection = BuildInspection([]TaskRecord{unknown}, mirror, registry, nil, false, true)
	if len(inspection.Tasks) != 1 || inspection.Tasks[0].Kind != TaskInspectionBlocked ||
		inspection.Tasks[0].Reason != BlockedMissingTask {
		t.Fatalf("tasks = %+v", inspection.Tasks)
	}
}
