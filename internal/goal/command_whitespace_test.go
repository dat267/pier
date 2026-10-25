package goal

import "testing"

// D223 reference: installed goal/command.ts parseGoalCommand uses JS trim,
// which trims BOM but not NEXT LINE. This is not a stock pi feature.
func TestGoalCommandTrimsJavaScriptWhitespace(t *testing.T) {
	for _, item := range []struct {
		input     string
		kind      GoalCommandKind
		objective string
	}{
		{"\ufeffstatus\ufeff", CmdShowStatus, ""},
		{"set \ufeffobjective\ufeff", CmdSet, "objective"},
		{"\u0085status\u0085", CmdError, ""},
	} {
		got := ParseGoalCommand(item.input)
		if got.Kind != item.kind || got.Objective != item.objective {
			t.Errorf("input %q: got %+v, want kind=%s objective=%q", item.input, got, item.kind, item.objective)
		}
	}
}
