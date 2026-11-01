package goal

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// D223: user-requested native presentation. pi 4a080252's
// packages/coding-agent/src/modes/interactive/components/tool-execution.ts
// stacks call/result components, while durable/event cards own their shell.
// Both layouts must separate nonempty sections with exactly one blank line.
func TestGoalLifecycleAndEventSectionSpacing(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		for _, tc := range []struct {
			op    GoalOperation
			label string
		}{
			{OpCreate, "created"}, {OpEdit, "edited"}, {OpPause, "paused"},
			{OpResume, "resumed"}, {OpComplete, "completed"}, {OpBlock, "blocked"},
		} {
			g := GoalSnapshot{Objective: "Ship the widget", Revision: 2, Phase: PhaseActive}
			want := "Goal " + tc.label + " rev 2\n\nShip the widget"
			if tc.op == OpBlock {
				g.Phase = PhaseBlocked
				g.BlockedReason = &BlockedReason{Code: "dependency", Message: "Waiting on access"}
				want += "\n\ndependency: Waiting on access"
			}
			card := RenderGoalChangeEntry(GoalChangeEntry{Operation: tc.op, Goal: &g}, stubTheme{}, expanded)
			if got := goalCardSections(card); got != want {
				t.Errorf("%s expanded=%v sections = %q, want %q", tc.label, expanded, got, want)
			}
		}
		clear := RenderGoalChangeEntry(GoalChangeEntry{Operation: OpClear}, stubTheme{}, expanded)
		if got, want := goalCardSections(clear), "Goal cleared\n\nGoal cleared. Durable history remains in the session log."; got != want {
			t.Errorf("clear sections = %q, want %q", got, want)
		}
		for _, kind := range []string{"round", "paused", "resumed", "complete", "blocked"} {
			card := RenderGoalEventMessage(kind, "Ship the widget", 0, false, PhaseActive, true, stubTheme{}, expanded)
			want := "Goal " + kind + "\n\nShip the widget"
			if !expanded && (kind == "complete" || kind == "blocked") {
				want = "Goal " + kind + " click or expand for full message"
			}
			if got := goalCardSections(card); got != want {
				t.Errorf("event %s expanded=%v sections = %q, want %q", kind, expanded, got, want)
			}
		}
	}
}

func TestGoalCreateErrorSeparatesFromCall(t *testing.T) {
	card := &tui.Container{}
	card.AddChild(RenderSetGoalRenderCall("Ship the widget", stubTheme{}, false))
	card.AddChild(RenderSetGoalCreateResult(SetGoalCreateResult{IsError: true, Text: "Could not save goal"}, stubTheme{}))
	want := "Set goal\n\nShip the widget\n\nCould not save goal"
	if got := goalCardSections(card); got != want {
		t.Fatalf("error sections = %q, want %q", got, want)
	}
}

func goalCardSections(card tui.Component) string {
	lines := card.Render(160)
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
