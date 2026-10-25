package goal

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// D223 reference: installed goal/render.ts measures JavaScript string length
// before truncating cards and deciding whether to show the expansion hint.
func TestGoalCardsUseJSLengthForExpansionHints(t *testing.T) {
	objective := strings.Repeat("😀", 29) + "abc"
	for _, item := range []struct {
		name string
		card tui.Component
	}{
		{"objective", RenderSetGoalRenderCall(objective, stubTheme{}, false)},
		{"reason", RenderSetGoalStatusRenderCall("blocked", objective, stubTheme{}, false)},
		{"body", RenderGoalCard(stubTheme{}, GoalCard{Label: "Goal", Body: strings.Repeat("😀", 30) + strings.Repeat("a", 21)}, false)},
	} {
		if got := strings.Join(item.card.Render(500), "\n"); !strings.Contains(got, "click or expand") {
			t.Errorf("%s: missing expansion hint in %q", item.name, got)
		}
	}
}
