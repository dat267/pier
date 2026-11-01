// This file follows goal/render.ts: pure functions that turn goal state into
// Box/Text trees.
package goal

import (
	"fmt"
	"regexp"
	"strings"
	"time"

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
		return &tui.TuiMouseDispatchResult{Handled: true}
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
	if !expanded {
		preview, hidden := goalPreview(body, false)
		body = preview
		if hidden > 0 {
			body += goalHiddenHint(hidden)
		}
	}
	if body != "" {
		body = "\n" + body
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

// goalPreviewLines is the collapsed body height for an objective or blocker: a
// card shows this many lines and how many it hides, like the shell preview.
// The cap counts lines, not characters, so one long line is shown whole.
const goalPreviewLines = 5

// goalPreview returns the text a collapsed card shows and the number of lines
// it hides. An expanded card keeps the text as written, including its own line
// structure; a collapsed one keeps its first lines unmangled rather than
// flattening them into a summary.
func goalPreview(text string, expanded bool) (shown string, hidden int) {
	trimmed := ai.JSTrim(text)
	if expanded || trimmed == "" {
		return trimmed, 0
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= goalPreviewLines {
		return trimmed, 0
	}
	return strings.Join(lines[:goalPreviewLines], "\n"), len(lines) - goalPreviewLines
}

// goalHiddenHint is the hidden-line note under a collapsed preview, in the
// shell card's "... (N earlier lines, ...)" shape. It names both ways to expand
// instead of the shell's keybinding-only hint, because a goal card opens on a
// click as well as on the expand key.
func goalHiddenHint(hidden int) string {
	return fmt.Sprintf("\n... (%d more lines, click or expand for full text)", hidden)
}

// RenderGetGoalRenderResult is the get_goal result card: the state line, then
// the objective and any budget or blocker detail, mirroring the multi-line body
// of the built-in tool cards.
func RenderGetGoalRenderResult(goal *GoalView, theme Theme, expanded bool) *tui.Text {
	if goal == nil {
		return tui.NewText(theme.Fg("muted", "No goal set"), 0, 0, nil)
	}
	state := fmt.Sprintf("%s · rev %d · %d rounds", goal.Phase, goal.Revision, goal.TurnsStarted)
	if goal.Phase == PhaseActive && !goal.Armed {
		state += " · disarmed"
	}
	stateColor := PhaseColor[goal.Phase]
	if stateColor == "" {
		stateColor = "success"
	}
	lines := []string{theme.Fg(stateColor, state)}

	objective, hidden := goalPreview(goal.Objective, expanded)
	lines = append(lines, theme.Fg("toolOutput", objective))
	if hidden > 0 {
		lines = append(lines, theme.Fg("muted", ai.JSTrim(goalHiddenHint(hidden))))
	}
	if goal.Budget != nil {
		elapsed := time.Now().UnixMilli() - goal.CreatedAt
		lines = append(lines, theme.Fg("muted", "budget: "+FormatBudget(goal.Budget, goal.UsedTokens, goal.UsedCostUsd, elapsed)))
	}
	if goal.BlockedReason != nil {
		stopColor := PhaseColor[goal.Phase]
		if stopColor == "" {
			stopColor = "warning"
		}
		lines = append(lines, theme.Fg(stopColor, "reason: "+goal.BlockedReason.Code+": "+goal.BlockedReason.Message))
	}
	return tui.NewText(strings.Join(lines, "\n"), 0, 0, nil)
}

// RenderSetGoalRenderCall is the set_goal call card when an objective is set:
// a title line and the objective as a wrapped body, like the write card.
func RenderSetGoalRenderCall(objective string, theme Theme, expanded bool) *tui.Text {
	shown, hidden := goalPreview(objective, expanded)
	if shown == "" {
		// Streaming arguments carry no objective yet; show a placeholder like
		// the shell card does instead of an empty body.
		shown = "…"
	}
	text := theme.Fg("toolTitle", theme.Bold("Set goal")) + "\n\n" + theme.Fg("toolOutput", shown)
	if hidden > 0 {
		text += theme.Fg("muted", goalHiddenHint(hidden))
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
		shown, hidden := goalPreview(blockedReason, expanded)
		text += "\n\n" + theme.Fg("toolOutput", shown)
		if hidden > 0 {
			text += theme.Fg("muted", goalHiddenHint(hidden))
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

// SetGoalCreateResult is the set_goal objective (create or replace) result card
// input. The call card carries the objective, so the result reports only the
// durable outcome.
type SetGoalCreateResult struct {
	IsError  bool
	Replaced bool
	HasGoal  bool
	Revision int
	Text     string
}

// RenderSetGoalCreateResult is the set_goal objective result card.
func RenderSetGoalCreateResult(result SetGoalCreateResult, theme Theme) *tui.Text {
	if result.IsError {
		return tui.NewText(theme.Fg("error", result.Text), 0, 0, nil)
	}
	label := "Goal created ✓"
	if result.Replaced {
		label = "Goal replaced ✓"
	}
	// Separate the outcome from the objective in the shared tool box.
	text := "\n" + theme.Fg(PhaseColor[PhaseActive], label)
	if result.HasGoal {
		text += theme.Fg("dim", fmt.Sprintf(" · rev %d", result.Revision))
	}
	return tui.NewText(text, 0, 0, nil)
}
