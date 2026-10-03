package durable

import (
	"testing"
)

// Port of the scheduler ownership/idle traversal (harness/scheduler.ts).

func traversalRecord(id Id, owner *Id, background bool, status string) TaskRecord {
	return TaskRecord{
		ID: id, ConversationID: 1, Version: 1, Owner: owner, Background: background,
		State: TaskState{Status: status},
	}
}

func idPointerOf(value Id) *Id { return &value }

func TestAboveWalk(t *testing.T) {
	owner2 := idPointerOf(2)
	graph := SchedulerGraph{
		Live: map[Id]TaskRecord{
			2: traversalRecord(2, nil, false, TaskRunning),
			3: traversalRecord(3, owner2, false, TaskRunning),
		},
		Edges: map[Id]*Id{1: nil},
	}
	start := SchedulerUp{Task: idPointerOf(3)}
	steps := Above(start, graph)
	if len(steps) != 3 {
		t.Fatalf("steps = %+v", steps)
	}
	if steps[0].Task == nil || *steps[0].Task != 3 || steps[1].Task == nil || *steps[1].Task != 2 {
		t.Fatalf("steps = %+v", steps)
	}
	if steps[2].Conversation == nil || *steps[2].Conversation != 1 || steps[2].Unknown {
		t.Fatalf("steps = %+v", steps)
	}
	if !ChainKnown(start, graph) {
		t.Fatal("the chain is known")
	}
	// An unloaded owner yields the unknown step.
	graph.Live = map[Id]TaskRecord{3: traversalRecord(3, owner2, false, TaskRunning)}
	steps = Above(start, graph)
	if len(steps) != 2 || !steps[1].Unknown || ChainKnown(start, graph) {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestOwnedLive(t *testing.T) {
	owner2 := idPointerOf(2)
	owner3 := idPointerOf(3)
	graph := SchedulerGraph{
		Live: map[Id]TaskRecord{
			2: traversalRecord(2, nil, false, TaskRunning),
			3: traversalRecord(3, owner2, false, TaskRunning),
			4: traversalRecord(4, owner3, false, TaskRunning),
			5: traversalRecord(5, owner2, true, TaskRunning),
		},
		Edges: map[Id]*Id{1: nil},
	}
	records := []TaskRecord{graph.Live[3], graph.Live[4], graph.Live[5]}
	owned := OwnedLive(records, graph)
	if len(owned[2]) != 2 || owned[2][0] != 3 || owned[2][1] != 4 {
		t.Fatalf("owned[2] = %+v", owned[2])
	}
	if len(owned[3]) != 1 || owned[3][0] != 4 {
		t.Fatalf("owned[3] = %+v", owned[3])
	}
	// The background task is skipped entirely.
	for _, below := range owned {
		for _, id := range below {
			if id == 5 {
				t.Fatalf("a background task must not count: %+v", owned)
			}
		}
	}
}

func TestInScope(t *testing.T) {
	owner2 := idPointerOf(2)
	graph := SchedulerGraph{
		Live: map[Id]TaskRecord{
			2: traversalRecord(2, nil, false, TaskRunning),
			3: traversalRecord(3, owner2, false, TaskRunning),
		},
		Edges: map[Id]*Id{1: nil},
	}
	start := SchedulerUp{Task: idPointerOf(3)}
	if value, known := InScope(start, SchedulerScope{Conversation: idPointerOf(1)}, false, graph); !value || !known {
		t.Fatalf("conversation scope = %v, %v", value, known)
	}
	if value, known := InScope(start, SchedulerScope{Roots: true}, false, graph); !value || !known {
		t.Fatalf("roots scope = %v, %v", value, known)
	}
	if value, _ := InScope(start, SchedulerScope{Conversation: idPointerOf(2)}, false, graph); value {
		t.Fatal("another conversation is out of scope")
	}
	// A background owner stops ordinary traversal.
	graph.Live[2] = traversalRecord(2, nil, true, TaskRunning)
	if value, known := InScope(start, SchedulerScope{Roots: true}, false, graph); value || !known {
		t.Fatalf("background stop = %v, %v", value, known)
	}
	if value, _ := InScope(start, SchedulerScope{Roots: true}, true, graph); !value {
		t.Fatal("crossBackground crosses it")
	}
}

func TestBelowCancelled(t *testing.T) {
	owner2 := idPointerOf(2)
	graph := SchedulerGraph{
		Live: map[Id]TaskRecord{
			2: traversalRecord(2, nil, false, TaskRunning),
			3: traversalRecord(3, owner2, false, TaskRunning),
		},
		Edges: map[Id]*Id{1: nil},
	}
	start := SchedulerUp{Task: idPointerOf(3)}
	if BelowCancelled(start, graph) {
		t.Fatal("no intent yet")
	}
	marked := graph.Live[2]
	marked.AbortRequested = true
	graph.Live[2] = marked
	if !BelowCancelled(start, graph) {
		t.Fatal("an abort-marked owner cascades")
	}
	// A background owner without intent stops the search.
	graph.Live[2] = traversalRecord(2, nil, true, TaskRunning)
	if BelowCancelled(start, graph) {
		t.Fatal("a background owner without intent stops")
	}
}

func TestWaitingOn(t *testing.T) {
	live := map[Id]TaskRecord{4: traversalRecord(4, nil, false, TaskRunning)}
	waiting := TaskRecord{State: TaskState{Status: TaskWaiting, On: []Id{3, 4}}}
	on := WaitingOn(waiting, nil, live)
	if len(on) != 1 || on[0] != 4 {
		t.Fatalf("on = %+v", on)
	}
	running := TaskRecord{State: TaskState{Status: TaskRunning, On: []Id{4}}}
	if on := WaitingOn(running, nil, live); on != nil {
		t.Fatalf("on = %+v", on)
	}
	marked := TaskRecord{AbortRequested: true, State: TaskState{Status: TaskRunning}}
	owned := map[Id][]Id{marked.ID: {2, 3}}
	if on := WaitingOn(marked, owned, live); len(on) != 2 || on[0] != 2 || on[1] != 3 {
		t.Fatalf("on = %+v", on)
	}
}
