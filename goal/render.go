package goal

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dat267/pier/tui"
)

// This file follows goal/render.ts: pure functions that turn goal state into
// Box/Text trees.

// Theme is the rendering surface the goal cards need (the port's
// interactive.Theme satisfies it, since ThemeColor is a string alias).
type Theme interface {
	Fg(color string, text string) string
	Bg(color string, text string) string
	Bold(text string) string
}

// PhaseColor maps a phase to its theme color.
var PhaseColor = map[GoalPhase]string{
	PhaseActive:   "success",
	PhasePaused:   "warning",
	PhaseBlocked:  "error",
	PhaseComplete: "accent",
}

var goalTagPattern = regexp.MustCompile(`</?goal_(round|complete|blocked)>\n?`)

// DisplayBody strips the goal wrapper tags for display.
func DisplayBody(content string) string {
	return strings.TrimSpace(goalTagPattern.ReplaceAllString(content, ""))
}

// GoalCard is the label/body/phase/detail of one goal card.
type GoalCard struct {
	Label    string
	Body     string
	Phase    GoalPhase
	HasPhase bool
	Detail   string
}

// RenderGoalCard builds a tinted goal card (Box with label + body).
func RenderGoalCard(theme Theme, card GoalCard, expanded bool) *tui.Box {
	// Box(1,1): tinted vertical padding like pi's own tool cards.
	box := tui.NewBox(1, 1, func(text string) string { return theme.Bg("customMessageBg", text) })
	label := card.Label
	if card.HasPhase {
		label = theme.Fg(PhaseColor[card.Phase], label)
	} else {
		label = theme.Fg("customMessageLabel", theme.Bold(label))
	}
	if card.Detail != "" {
		label += theme.Fg("dim", " "+card.Detail)
	}
	box.AddChild(tui.NewText(label, 0, 0, nil))
	body := card.Body
	if !expanded {
		body = TruncateObjective(body, 80)
	}
	box.AddChild(tui.NewText(theme.Fg("customMessageText", body), 0, 0, nil))
	return box
}

var goalOperationLabels = map[GoalOperation]string{
	OpCreate:   "created",
	OpEdit:     "edited",
	OpPause:    "paused",
	OpResume:   "resumed",
	OpComplete: "completed",
	OpBlock:    "blocked",
	OpClear:    "cleared",
}

// RenderGoalChangeEntry renders a durable lifecycle entry as a card.
func RenderGoalChangeEntry(data GoalChangeEntry, theme Theme, expanded bool) *tui.Box {
	var phase GoalPhase
	hasPhase := false
	if data.Goal != nil {
		phase = data.Goal.Phase
		hasPhase = true
	} else if data.Operation == OpClear {
		phase = PhaseComplete
		hasPhase = true
	}
	body := "Goal cleared. Durable history remains in the session log."
	detail := ""
	if data.Goal != nil {
		body = data.Goal.Objective
		if data.Goal.BlockedReason != nil {
			body += "\n" + data.Goal.BlockedReason.Code + ": " + data.Goal.BlockedReason.Message
		}
		detail = fmt.Sprintf("rev %d", data.Goal.Revision)
	}
	return RenderGoalCard(theme, GoalCard{
		Label:    "Goal " + goalOperationLabels[data.Operation],
		Body:     body,
		Phase:    phase,
		HasPhase: hasPhase,
		Detail:   detail,
	}, expanded)
}

var goalEventLabels = map[string]string{
	"round":    "Goal round",
	"paused":   "Goal paused",
	"blocked":  "Goal blocked",
	"complete": "Goal complete",
	"resumed":  "Goal resumed",
}

// RenderGoalEventMessage renders a continuation/wrap-up message as a card.
func RenderGoalEventMessage(kind, content string, turn int, hasTurn bool, currentPhase GoalPhase, theme Theme, expanded bool) *tui.Box {
	// Wrap-up cards repeat the objective already shown by the durable entry
	// card — collapse to label-only when not expanded.
	wrapup := kind == "complete" || kind == "blocked"
	body := DisplayBody(content)
	if wrapup && !expanded {
		body = ""
	}
	phase := currentPhase
	hasPhase := phase != ""
	switch kind {
	case "blocked":
		phase, hasPhase = PhaseBlocked, true
	case "complete":
		phase, hasPhase = PhaseComplete, true
	}
	detail := ""
	if kind == "round" && hasTurn {
		detail = fmt.Sprintf("#%d", turn)
	}
	label := goalEventLabels[kind]
	if label == "" {
		label = "Goal"
	}
	return RenderGoalCard(theme, GoalCard{
		Label:    label,
		Body:     body,
		Phase:    phase,
		HasPhase: hasPhase,
		Detail:   detail,
	}, expanded)
}

// RenderGoalTurnEntry renders an admitted goal turn entry — one flat line;
// tinted padding would stack with the next component's top spacer.
func RenderGoalTurnEntry(data GoalTurnEntry, theme Theme, expanded bool) *tui.Text {
	detail := ""
	if expanded {
		detail = fmt.Sprintf(" · goal %s rev %d", data.GoalID, data.Revision)
	}
	return tui.NewText(theme.Fg(PhaseColor[PhaseActive], fmt.Sprintf("Goal round admitted #%d", data.Turn))+theme.Fg("dim", detail), 1, 0, nil)
}

// RenderGetGoalRenderCall is the get_goal call card.
func RenderGetGoalRenderCall(theme Theme) *tui.Text {
	return tui.NewText(theme.Fg("toolTitle", "Get goal"), 0, 0, nil)
}

// RenderGetGoalRenderResult is the get_goal result card.
func RenderGetGoalRenderResult(goal *GoalView, theme Theme) *tui.Text {
	if goal == nil {
		return tui.NewText(theme.Fg("muted", "No goal set"), 0, 0, nil)
	}
	return tui.NewText(theme.Fg("toolTitle", fmt.Sprintf("%s · rev %d · %d rounds", goal.Phase, goal.Revision, goal.TurnsStarted)), 0, 0, nil)
}

// RenderCreateGoalRenderCall is the create_goal call card.
func RenderCreateGoalRenderCall(objective string, theme Theme) *tui.Text {
	return tui.NewText(theme.Fg("toolTitle", "Create goal: "+TruncateObjective(objective, 60)), 0, 0, nil)
}

// RenderUpdateGoalRenderCall is the update_goal call card.
func RenderUpdateGoalRenderCall(action, blockedReason string, theme Theme) *tui.Text {
	text := theme.Fg("toolTitle", "Update goal → "+action)
	if action == "" {
		text = theme.Fg("toolTitle", "Update goal → ?")
	}
	if blockedReason != "" {
		text += theme.Fg("dim", ": "+TruncateObjective(blockedReason, 60))
	}
	return tui.NewText(text, 0, 0, nil)
}

// UpdateGoalResult is the update_goal result card input.
type UpdateGoalResult struct {
	IsError  bool
	Phase    GoalPhase
	HasPhase bool
	Text     string
}

// RenderUpdateGoalRenderResult is the update_goal result card.
func RenderUpdateGoalRenderResult(result UpdateGoalResult, theme Theme) *tui.Text {
	if result.IsError {
		return tui.NewText(theme.Fg("error", result.Text), 0, 0, nil)
	}
	// Success repeats the durable entry card right below — collapse to a
	// phase-colored status line instead of the full instruction text.
	phase := result.Phase
	if !result.HasPhase {
		phase = PhaseComplete
	}
	short := "Goal complete ✓"
	if phase == PhaseBlocked {
		short = "Goal blocked ✓"
	}
	return tui.NewText(theme.Fg(PhaseColor[phase], short), 0, 0, nil)
}
