package goal

import (
	"strings"
	"testing"
)

// blocked against a goal that already finished reported "Cannot block before 3
// consecutive goal rounds (current: 0)": the round-count guard answers first
// and sends the model back to work it has already finished. The reference
// extension reaches neither guard usefully; the port's own refusals name the
// state (D420).
func TestSetGoalBlockedOnAFinishedGoal(t *testing.T) {
	m := NewGoalMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalSetEvent{Objective: "ship it"})
	view := m.Snapshot().Goal
	m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "complete"})

	view = m.Snapshot().Goal
	result := m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "blocked", BlockedReason: "stuck"})
	if !result.IsError {
		t.Fatal("blocking a finished goal must be rejected")
	}
	reply := derefString(result.Reply)
	if strings.Contains(reply, "consecutive goal rounds") {
		t.Fatalf("reply sends the model back to work on a finished goal: %q", reply)
	}
	if !strings.Contains(reply, "already complete") {
		t.Fatalf("reply %q does not name the state", reply)
	}
}
