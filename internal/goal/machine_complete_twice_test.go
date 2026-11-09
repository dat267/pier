package goal

import (
	"strings"
	"testing"
)

// A second complete on a finished goal reported the phase graph's own
// transition error ("illegal transition complete -> complete"). Every sibling
// refusal in the set_goal status path names the current state and what the
// model can do instead, and this one is reachable: a real session called
// set_goal status=complete on a goal that was already complete. The reply is
// the tool result the model reads, so it must say that the goal has finished
// and that setting an objective starts a new one.
func TestSetGoalCompleteOnAFinishedGoal(t *testing.T) {
	m := NewGoalMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalSetEvent{Objective: "ship it"})
	view := m.Snapshot().Goal
	first := m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "complete"})
	if first.IsError {
		t.Fatalf("first complete rejected: %v", derefString(first.Reply))
	}

	view = m.Snapshot().Goal
	second := m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "complete"})
	if !second.IsError {
		t.Fatal("completing a finished goal must be reported, not silently accepted")
	}
	reply := derefString(second.Reply)
	if strings.Contains(reply, "illegal transition") {
		t.Fatalf("reply leaks the phase graph: %q", reply)
	}
	for _, want := range []string{"already complete", "objective"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("reply %q does not mention %q", reply, want)
		}
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
