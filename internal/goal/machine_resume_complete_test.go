package goal

import (
	"strings"
	"testing"
)

// /goal resume on a finished goal reached the state validator and reported
// "illegal transition complete -> active", the phase graph's wording (D420).
// A finished goal is not a stopped goal, so the guard above says so instead.
func TestGoalResumeOnAFinishedGoal(t *testing.T) {
	m := NewGoalMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalSetEvent{Objective: "ship it"})
	view := m.Snapshot().Goal
	m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "complete"})

	result := m.Dispatch(GoalResumeEvent{})
	if !result.IsError {
		t.Fatal("resuming a finished goal must be rejected")
	}
	reply := derefString(result.Reply)
	if strings.Contains(reply, "illegal transition") {
		t.Fatalf("reply leaks the phase graph: %q", reply)
	}
	if !strings.Contains(reply, "complete") {
		t.Fatalf("reply %q does not name the state", reply)
	}
}
