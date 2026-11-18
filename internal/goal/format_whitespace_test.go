package goal

import (
	"testing"
)

// D223 reference: installed goal/state.ts truncateObjective counts UTF-16 code
// units and passes max - 1 to String.slice, including zero/negative limits.
func TestGoalObjectiveTruncationUsesJSStringUnits(t *testing.T) {
	minInt := -int(^uint(0)>>1) - 1
	for _, item := range []struct {
		input string
		max   int
		want  string
	}{
		{"😀abc", 3, "😀…"},
		{"abc", 0, "ab…"},
		{"abc", -1, "a…"},
		{"abc", minInt, "…"},
	} {
		if got := TruncateObjective(item.input, item.max); got != item.want {
			t.Errorf("TruncateObjective(%q, %d) = %q, want %q", item.input, item.max, got, item.want)
		}
	}
}

// D223 reference: installed goal/state.ts truncateObjective flattens JavaScript
// /\s+/ then trims. BOM is whitespace; NEXT LINE is not.
func TestGoalObjectiveFormattingUsesJSWhitespace(t *testing.T) {
	for _, item := range []struct{ input, want string }{
		{"\ufeffA\u00a0\u2003B\ufeff", "A B"},
		{"\u0085A\u0085", "\u0085A\u0085"},
	} {
		if got := TruncateObjective(item.input, 60); got != item.want {
			t.Errorf("TruncateObjective(%q) = %q, want %q", item.input, got, item.want)
		}
	}
}

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

// D223 reference: installed goal/render.ts displayBody strips wrapper tags
// and applies JS trim, which removes BOM but preserves NEXT LINE.
func TestGoalDisplayBodyUsesJSWhitespace(t *testing.T) {
	for _, item := range []struct{ input, want string }{
		{"\ufeff<goal_round>\nbody\n</goal_round>\ufeff", "body"},
		{"\u0085<goal_round>\nbody</goal_round>\u0085", "\u0085body\u0085"},
	} {
		if got := DisplayBody(item.input); got != item.want {
			t.Errorf("DisplayBody(%q)=%q, want %q", item.input, got, item.want)
		}
	}
}
