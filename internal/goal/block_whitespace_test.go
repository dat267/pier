package goal

import "testing"

// D223 reference: installed goal/machine.ts goalUpdate trims blockedReason
// with String.trim before requiring a nonempty reason.
func TestGoalBlockReasonUsesJSWhitespace(t *testing.T) {
	for _, item := range []struct {
		reason string
		valid  bool
	}{
		{"\ufeff", false},
		{"\u0085", true},
	} {
		m := armedMachine(t)
		for range 3 {
			m.Dispatch(AgentEndEvent{})
			m.Dispatch(AgentBeforeSettleEvent{Outcome: "completed"})
		}
		m.Dispatch(AgentEndEvent{})
		ref := m.Snapshot().Goal
		result := m.Dispatch(GoalUpdateEvent{GoalID: ref.ID, Revision: ref.Revision, Action: "blocked", BlockedReason: item.reason})
		if result.IsError == item.valid {
			t.Errorf("reason %q: isError=%v, want valid=%v: %s", item.reason, result.IsError, item.valid, replyOf(result))
		}
	}
}
