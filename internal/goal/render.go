// This file follows goal/render.ts: pure functions that turn goal state into
// Box/Text trees.
package goal

import (
	"fmt"
	"regexp"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

// Theme is the rendering surface the goal cards need. The port's
// interactive.Theme satisfies it, since ThemeColor and ThemeBg are string
// aliases.
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

// ClickToExpand wraps a renderer so a left click toggles the expanded state,
// rebuilding the child on each toggle.
func ClickToExpand(render func(expanded bool) tui.Component, initiallyExpanded bool) tui.Component {
	mutable := &mutableGoalChild{render: render, expanded: initiallyExpanded}
	mutable.child = render(initiallyExpanded)
	region := tui.NewMouseRegion(mutable, func(event tui.TuiMouseEvent) *tui.TuiMouseDispatchResult {
		if event.Type != tui.MouseClick || event.Button != tui.MouseButtonLeft {
			return nil
		}
		mutable.expanded = !mutable.expanded
		mutable.child = render(mutable.expanded)
		return &tui.TuiMouseDispatchResult{TuiMouseEventResult: tui.TuiMouseEventResult{Handled: true}}
	})
	return &clickToExpandComponent{mutable: mutable, region: region}
}

type mutableGoalChild struct {
	render   func(expanded bool) tui.Component
	expanded bool
	child    tui.Component
}

func (m *mutableGoalChild) Render(width int) []string { return m.child.Render(width) }

func (m *mutableGoalChild) Invalidate() {
	m.child = m.render(m.expanded)
	m.child.Invalidate()
}

type clickToExpandComponent struct {
	mutable *mutableGoalChild
	region  *tui.MouseRegion
}

func (c *clickToExpandComponent) Render(width int) []string { return c.region.Render(width) }

func (c *clickToExpandComponent) Invalidate() { c.region.Invalidate() }

func (c *clickToExpandComponent) HandleMouse(event tui.TuiMouseEvent) *tui.TuiMouseDispatchResult {
	return c.region.HandleMouse(event)
}

var goalTagPattern = regexp.MustCompile(`</?goal_(round|complete|blocked|paused)>\n?`)

// DisplayBody strips the goal wrapper tags for display.
func DisplayBody(content string) string {
	return ai.JSTrim(goalTagPattern.ReplaceAllString(content, ""))
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
	if !expanded && ai.JSLength(body) > 80 {
		body = TruncateObjective(body, 64) + " (click or expand for full text)"
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
func RenderGoalEventMessage(kind, content string, turn int, hasTurn bool, currentPhase GoalPhase, hasPhase bool, theme Theme, expanded bool) *tui.Box {
	// Wrap-up cards repeat the objective already shown by the durable entry
	// card — collapse to label-only when not expanded.
	wrapup := kind == "complete" || kind == "blocked"
	body := DisplayBody(content)
	if wrapup && !expanded {
		body = ""
	}
	phase := currentPhase
	phaseSet := hasPhase
	switch kind {
	case "blocked":
		phase, phaseSet = PhaseBlocked, true
	case "complete":
		phase, phaseSet = PhaseComplete, true
	}
	detail := ""
	if wrapup && !expanded {
		detail = "click or expand for full message"
	} else if kind == "round" && hasTurn {
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
		HasPhase: phaseSet,
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

// RenderSetGoalRenderCall is the set_goal call card when an objective is set.
func RenderSetGoalRenderCall(objective string, theme Theme, expanded bool) *tui.Text {
	normalized := ai.JSTrim(goalWhitespace.ReplaceAllString(objective, " "))
	truncated := !expanded && ai.JSLength(normalized) > 60
	shown := objective
	if !expanded {
		shown = TruncateObjective(objective, 60)
	}
	text := theme.Fg("toolTitle", "Set goal: "+shown)
	if truncated {
		text += theme.Fg("muted", " (click or expand for full objective)")
	}
	return tui.NewText(text, 0, 0, nil)
}

// RenderSetGoalStatusRenderCall is the set_goal call card when a status ends
// the goal.
func RenderSetGoalStatusRenderCall(action, blockedReason string, theme Theme, expanded bool) *tui.Text {
	text := theme.Fg("toolTitle", "Set goal → ")
	if action == "" {
		text += theme.Fg("toolTitle", "?")
	} else {
		text += theme.Fg("toolTitle", action)
	}
	if blockedReason != "" {
		normalized := ai.JSTrim(goalWhitespace.ReplaceAllString(blockedReason, " "))
		truncated := !expanded && ai.JSLength(normalized) > 60
		shown := normalized
		if !expanded {
			shown = TruncateObjective(normalized, 60)
		}
		text += theme.Fg("dim", ": "+shown)
		if truncated {
			text += theme.Fg("muted", " (click or expand for full reason)")
		}
	}
	return tui.NewText(text, 0, 0, nil)
}

// SetGoalResult is the set_goal status result card input.
type SetGoalResult struct {
	IsError  bool
	Phase    GoalPhase
	HasPhase bool
	// Action is the requested set_goal status, so a clear (D232) renders
	// differently from a completion.
	Action string
	Text   string
}

// RenderSetGoalRenderResult is the set_goal status result card.
func RenderSetGoalRenderResult(result SetGoalResult, theme Theme) *tui.Text {
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
	switch {
	case result.Action == "clear":
		short = "Goal cleared ✓"
	case phase == PhaseBlocked:
		short = "Goal blocked ✓"
	}
	return tui.NewText(theme.Fg(PhaseColor[phase], short), 0, 0, nil)
}
