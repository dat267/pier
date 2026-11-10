package goal

import (
	"strings"
	"testing"
)

// An unknown action is echoed back to the model. The sibling command path
// already bounds its echo with TruncateObjective(trimmed, 20)
// (internal/goal/command.go:54); the machine path echoed the value whole, so a
// 100,000-character status produced a 100,057-character reply (D432).
func TestUnknownActionReplyIsBounded(t *testing.T) {
	m := NewGoalMachine()
	m.Dispatch(GoalAgentSetEvent{Objective: "o"})
	current := m.Snapshot()
	res := m.Dispatch(GoalUpdateEvent{
		GoalID:   current.Goal.ID,
		Revision: float64(current.Goal.Revision),
		Action:   strings.Repeat("z", 100000),
	})
	if res.Reply == nil {
		t.Fatal("no reply")
	}
	if len(*res.Reply) > 120 {
		t.Fatalf("reply is %d characters for a 100,000-character action", len(*res.Reply))
	}
	if !strings.Contains(*res.Reply, "Unknown action") {
		t.Fatalf("reply lost its message: %q", *res.Reply)
	}
}

// A short typo is still shown as the caller wrote it.
func TestUnknownActionReplyKeepsAShortValue(t *testing.T) {
	m := NewGoalMachine()
	m.Dispatch(GoalAgentSetEvent{Objective: "o"})
	current := m.Snapshot()
	res := m.Dispatch(GoalUpdateEvent{
		GoalID:   current.Goal.ID,
		Revision: float64(current.Goal.Revision),
		Action:   "complet",
	})
	if res.Reply == nil || !strings.Contains(*res.Reply, `"complet"`) {
		t.Fatalf("short action was not quoted back: %v", res.Reply)
	}
}
