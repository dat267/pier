package goal

import "testing"

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
