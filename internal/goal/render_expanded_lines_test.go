package goal

import (
	"strings"
	"testing"
)

// joinTrimmed renders a card and drops the per-line padding Text adds, so a
// test can compare the text's own line structure.
func joinTrimmed(component interface{ Render(int) []string }, width int) string {
	lines := component.Render(width)
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " ")
	}
	return strings.Join(lines, "\n")
}

// Expanding a goal card must show the stored text as written: the collapsed
// preview flattens whitespace so it fits on a summary line, but an expanded
// objective keeps its own line structure (D223; the entry card and the /goal
// status message both preserve it).
func TestExpandedGoalCardsKeepLineBreaks(t *testing.T) {
	objective := "Ship the renderer\nwith two acceptance lines\nand a verification step"
	create := joinTrimmed(RenderSetGoalRenderCall(objective, stubTheme{}, true), 200)
	if !strings.Contains(create, objective) {
		t.Fatalf("expanded create card flattened the objective lines: %q", create)
	}

	reason := "First blocker line\nsecond blocker line"
	status := joinTrimmed(RenderSetGoalStatusRenderCall("blocked", reason, stubTheme{}, true), 200)
	if !strings.Contains(status, reason) {
		t.Fatalf("expanded status card flattened the reason lines: %q", status)
	}

	view := GoalView{GoalSnapshot: GoalSnapshot{Revision: 1, Objective: objective, Phase: PhaseActive}, Armed: true}
	get := joinTrimmed(RenderGetGoalRenderResult(&view, stubTheme{}, true), 200)
	if !strings.Contains(get, objective) {
		t.Fatalf("expanded get_goal card flattened the objective lines: %q", get)
	}

	// The collapsed card still flattens to a single preview line.
	collapsed := joinTrimmed(RenderSetGoalRenderCall(objective, stubTheme{}, false), 200)
	if strings.Contains(collapsed, "Ship the renderer\n") || !strings.Contains(collapsed, "Ship the renderer with two acceptance lines and a verification step") {
		t.Fatalf("collapsed create card = %q", collapsed)
	}
}
