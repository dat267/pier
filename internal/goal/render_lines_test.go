package goal

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// D223, user-requested layout: a collapsed card is capped by lines like the
// shell preview, not by characters, so a long objective keeps its line
// structure and its first lines instead of a mid-word cut.
func TestCollapsedGoalCardsCapByLines(t *testing.T) {
	objective := strings.Join([]string{
		"first line", "second line", "third line", "fourth line", "fifth line",
		"sixth line", "seventh line",
	}, "\n")
	for _, test := range []struct {
		name string
		card tui.Component
	}{
		{"call", RenderSetGoalRenderCall(objective, stubTheme{}, false)},
		{"reason", RenderSetGoalStatusRenderCall("blocked", objective, stubTheme{}, false)},
		{"get", RenderGetGoalRenderResult(&GoalView{GoalSnapshot: GoalSnapshot{
			Revision: 1, Objective: objective, Phase: PhaseActive,
		}, Armed: true}, stubTheme{}, false)},
		{"entry", RenderGoalChangeEntry(GoalChangeEntry{
			Operation: OpCreate, Goal: &GoalSnapshot{
				ID: "g1", Revision: 1, Objective: objective, Phase: PhaseActive,
			},
		}, stubTheme{}, false)},
	} {
		got := renderLines(test.card, 200)
		for _, want := range []string{"first line", "fifth line", "(2 more lines"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %q missing %q", test.name, got, want)
			}
		}
		if strings.Contains(got, "sixth line") || strings.Contains(got, "seventh line") {
			t.Errorf("%s: collapsed card shows lines past the cap: %q", test.name, got)
		}
		if !strings.Contains(got, "click or expand") {
			t.Errorf("%s: missing expansion hint: %q", test.name, got)
		}
	}
}

// D427: a single over-wide line is clipped and counted as hidden. Keeping it
// whole drew 12,502 rendered lines for a 1 MB objective, because tui.Text
// wraps the line to the transcript width. The line structure is still kept:
// clipping truncates, it does not re-wrap or rejoin.
func TestCollapsedGoalCardClipsOneOverWideLine(t *testing.T) {
	objective := strings.TrimSpace(strings.Repeat("acceptance criterion ", 40))
	got := flatten(RenderSetGoalRenderCall(objective, stubTheme{}, false), 400)
	if strings.Contains(got, objective) {
		t.Fatalf("a %d-byte single line was shown whole: %q", len(objective), got)
	}
	if !strings.Contains(got, "acceptance criterion") {
		t.Fatalf("the start of the line should survive: %q", got)
	}
	if !strings.Contains(got, "click or expand") {
		t.Fatalf("a clipped line must offer expansion: %q", got)
	}
	expanded := flatten(RenderSetGoalRenderCall(objective, stubTheme{}, true), 400)
	if !strings.Contains(expanded, objective) {
		t.Fatalf("expanded card lost the objective")
	}
}
