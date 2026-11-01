package goal

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// D223, user-requested layout: the collapsed preview counts lines, so a card
// with several lines offers the hint whether its content is ASCII or astral.
// One long line is never cut, matching the shell preview's line-based cap.
func TestGoalCardsCapPreviewByLines(t *testing.T) {
	objective := strings.Join([]string{
		strings.Repeat("😀", 30), "second", "third", "fourth", "fifth", "sixth", "seventh",
	}, "\n")
	for _, item := range []struct {
		name string
		card tui.Component
	}{
		{"objective", RenderSetGoalRenderCall(objective, stubTheme{}, false)},
		{"reason", RenderSetGoalStatusRenderCall("blocked", objective, stubTheme{}, false)},
		{"body", RenderGoalCard(stubTheme{}, GoalCard{Label: "Goal", Body: objective}, false)},
	} {
		got := strings.Join(item.card.Render(500), "\n")
		if !strings.Contains(got, "(2 more lines") || !strings.Contains(got, "click or expand") {
			t.Errorf("%s: missing line-count expansion hint in %q", item.name, got)
		}
		if strings.Contains(got, "seventh") {
			t.Errorf("%s: showed a line past the cap: %q", item.name, got)
		}
	}
	single := RenderSetGoalRenderCall(strings.Repeat("😀", 79)+"abc", stubTheme{}, false)
	if got := strings.Join(single.Render(500), "\n"); strings.Contains(got, "click or expand") {
		t.Errorf("a single line must not offer expansion: %q", got)
	}
}
