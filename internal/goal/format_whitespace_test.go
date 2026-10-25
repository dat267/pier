package goal

import "testing"

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
