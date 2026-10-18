package goal

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// Mirrors ~/.pi/agent/extensions/goal/render.test.ts.

// stubTheme returns strings unchanged, so assertions verify structure.
type stubTheme struct{}

func (stubTheme) Fg(_ string, text string) string { return text }
func (stubTheme) Bg(_ string, text string) string { return text }
func (stubTheme) Bold(text string) string         { return text }

func renderLines(component tui.Component, width int) string {
	return strings.TrimRight(strings.Join(component.Render(width), "\n"), " \n")
}

func TestDisplayBodyStripsGoalTags(t *testing.T) {
	cases := map[string]string{
		"<goal_round>\nHello\n":                  "Hello",
		"</goal_complete>\nDone":                 "Done",
		"<goal_blocked>\nStuck\n</goal_blocked>": "Stuck",
		"Hello world":                            "Hello world",
	}
	for input, want := range cases {
		if got := DisplayBody(input); got != want {
			t.Fatalf("%q: got %q want %q", input, got, want)
		}
	}
}

func TestPhaseColorHasAllFourPhases(t *testing.T) {
	want := map[GoalPhase]string{PhaseActive: "success", PhasePaused: "warning", PhaseBlocked: "error", PhaseComplete: "accent"}
	for phase, color := range want {
		if PhaseColor[phase] != color {
			t.Fatalf("%s: got %q", phase, PhaseColor[phase])
		}
	}
}

func TestRenderGoalCardReturnsABoxWithTwoChildren(t *testing.T) {
	card := RenderGoalCard(stubTheme{}, GoalCard{Label: "test", Body: "hello"}, false)
	if len(card.Children) != 2 {
		t.Fatalf("children=%d", len(card.Children))
	}
}

func TestRenderGoalCardTruncatesAndHints(t *testing.T) {
	long := strings.Repeat("a", 200)
	card := RenderGoalCard(stubTheme{}, GoalCard{Label: "test", Body: long}, false)
	rendered := strings.TrimRight(renderLines(card.Children[1], 500), "\n")
	if !strings.Contains(rendered, "click or expand for full text") {
		t.Fatalf("missing hint: %q", rendered)
	}
	if len(rendered) >= len(long) {
		t.Fatalf("not truncated: %q", rendered)
	}
}

func TestRenderGoalCardShowsCompleteBodyWhenExpanded(t *testing.T) {
	long := strings.Repeat("a", 200)
	card := RenderGoalCard(stubTheme{}, GoalCard{Label: "test", Body: long}, true)
	rendered := strings.TrimRight(renderLines(card.Children[1], 500), "\n")
	if rendered != long {
		t.Fatalf("got %q", rendered)
	}
}

func TestRenderGoalChangeEntryCards(t *testing.T) {
	g := createTestGoal("test objective", t0, nil)
	create := RenderGoalChangeEntry(GoalChangeEntry{Operation: OpCreate, Goal: &g, Timestamp: t0}, stubTheme{}, false)
	if len(create.Children) != 2 {
		t.Fatal("create entry must render a card")
	}
	clear := RenderGoalChangeEntry(GoalChangeEntry{Operation: OpClear, Cleared: &ClearedRef{ID: "g1", Revision: 1}, Timestamp: t0}, stubTheme{}, false)
	if len(clear.Children) != 2 {
		t.Fatal("clear entry must render a card")
	}
	blocked := g
	blocked.Phase = PhaseBlocked
	blocked.BlockedReason = &BlockedReason{Code: "err", Message: "stuck"}
	block := RenderGoalChangeEntry(GoalChangeEntry{Operation: OpBlock, Goal: &blocked, Timestamp: t0}, stubTheme{}, false)
	if len(block.Children) != 2 {
		t.Fatal("blocked entry must render a card")
	}
}

func TestRenderGoalEventMessageVariants(t *testing.T) {
	round := RenderGoalEventMessage("round", "<goal_round>\nDo work", 2, true, PhaseActive, true, stubTheme{}, false)
	if len(round.Children) != 2 {
		t.Fatal("round event must render a card")
	}
	complete := RenderGoalEventMessage("complete", "Done", 0, false, PhaseComplete, true, stubTheme{}, false)
	if len(complete.Children) != 2 {
		t.Fatal("complete event must render a card")
	}
	if !strings.Contains(renderLines(complete, 200), "click or expand for full message") {
		t.Fatal("wrap-up event must hint at expansion")
	}
	for _, kind := range []string{"complete", "blocked"} {
		card := RenderGoalEventMessage(kind, "The objective text", 0, false, PhaseComplete, true, stubTheme{}, false)
		if strings.Contains(renderLines(card, 200), "objective text") {
			t.Fatalf("%s card leaked body when collapsed", kind)
		}
		expanded := RenderGoalEventMessage(kind, "The objective text", 0, false, PhaseComplete, true, stubTheme{}, true)
		if !strings.Contains(renderLines(expanded, 200), "objective text") {
			t.Fatalf("%s card lost body when expanded", kind)
		}
	}
	blocked := RenderGoalEventMessage("blocked", "Stuck", 0, false, PhaseBlocked, true, stubTheme{}, false)
	if len(blocked.Children) != 2 {
		t.Fatal("blocked event must render a card")
	}
	paused := RenderGoalEventMessage("paused", "Paused", 0, false, PhasePaused, true, stubTheme{}, false)
	if len(paused.Children) != 2 {
		t.Fatal("paused event must render a card")
	}
	pausedBody := RenderGoalEventMessage("paused", "<goal_paused>\nPaused\n</goal_paused>", 0, false, PhasePaused, true, stubTheme{}, true)
	if strings.Contains(renderLines(pausedBody, 200), "goal_paused") {
		t.Fatal("paused event must strip its wrapper tags")
	}
}

func TestRenderGoalTurnEntryIsAFlatLine(t *testing.T) {
	card := RenderGoalTurnEntry(GoalTurnEntry{GoalID: "g1", Revision: 1, Turn: 3, Timestamp: t0}, stubTheme{}, false)
	out := card.Render(80)
	if len(out) != 1 {
		t.Fatalf("expected one line, got %v", out)
	}
	if !strings.Contains(out[0], "#3") {
		t.Fatalf("label missing round number: %q", out[0])
	}
	if !strings.HasPrefix(out[0], " ") {
		t.Fatalf("expected one-space left padding: %q", out[0])
	}
}

func TestToolRenderers(t *testing.T) {
	if RenderGetGoalRenderCall(stubTheme{}) == nil {
		t.Fatal("get_goal call")
	}
	if RenderGetGoalRenderResult(nil, stubTheme{}) == nil {
		t.Fatal("get_goal nil result")
	}
	g := GoalView{GoalSnapshot: createTestGoal("test", t0, nil), TurnsStarted: 2}
	if RenderGetGoalRenderResult(&g, stubTheme{}) == nil {
		t.Fatal("get_goal result")
	}
	if RenderSetGoalRenderCall("test", stubTheme{}, false) == nil {
		t.Fatal("set_goal objective call")
	}
	if RenderSetGoalStatusRenderCall("complete", "", stubTheme{}, false) == nil {
		t.Fatal("set_goal status call")
	}
	if RenderSetGoalRenderResult(SetGoalResult{Text: "ok"}, stubTheme{}) == nil {
		t.Fatal("set_goal result")
	}
	if RenderSetGoalRenderResult(SetGoalResult{IsError: true, Text: "fail"}, stubTheme{}) == nil {
		t.Fatal("set_goal error result")
	}
}

func TestCreateGoalCallFrameExpands(t *testing.T) {
	objective := "Create a long goal with every acceptance criterion, constraint, expected output, and verification step included."
	collapsed := renderLines(RenderSetGoalRenderCall(objective, stubTheme{}, false), 200)
	if !strings.Contains(collapsed, "click or expand") || strings.Contains(collapsed, objective) {
		t.Fatalf("collapsed frame: %q", collapsed)
	}
	expanded := renderLines(RenderSetGoalRenderCall(objective, stubTheme{}, true), 200)
	if !strings.Contains(expanded, objective) {
		t.Fatalf("expanded frame missing objective: %q", expanded)
	}
}

func TestUpdateGoalCallFrameExpands(t *testing.T) {
	reason := "The external dependency is unavailable, and the task cannot proceed until the service is restored."
	collapsed := renderLines(RenderSetGoalStatusRenderCall("blocked", reason, stubTheme{}, false), 200)
	if !strings.Contains(collapsed, "click or expand") || strings.Contains(collapsed, reason) {
		t.Fatalf("collapsed frame: %q", collapsed)
	}
	expanded := renderLines(RenderSetGoalStatusRenderCall("blocked", reason, stubTheme{}, true), 200)
	if !strings.Contains(expanded, reason) {
		t.Fatalf("expanded frame missing reason: %q", expanded)
	}
}

func TestClickToExpandTogglesOnLeftClick(t *testing.T) {
	objective := "complete the goal objective with all acceptance criteria and include all necessary verification steps"
	render := func(expanded bool) tui.Component {
		return RenderGoalChangeEntry(GoalChangeEntry{
			Operation: OpCreate,
			Goal:      &GoalSnapshot{ID: "g1", Revision: 1, Objective: objective, Phase: PhaseActive, CreatedAt: t0, UpdatedAt: t0},
			Timestamp: t0,
		}, stubTheme{}, expanded)
	}
	frame := ClickToExpand(render, false)
	before := renderLines(frame, 300)
	if !strings.Contains(before, "expand for full text") {
		t.Fatalf("expected collapsed frame: %q", before)
	}
	handled := frame.(interface {
		HandleMouse(tui.TuiMouseEvent) *tui.TuiMouseDispatchResult
	}).HandleMouse(tui.TuiMouseEvent{Type: tui.MouseClick, Button: tui.MouseButtonLeft, X: 2, Y: 1})
	if handled == nil || !handled.Handled {
		t.Fatal("left click must be handled")
	}
	expanded := renderLines(frame, 300)
	if !strings.Contains(expanded, objective) {
		t.Fatalf("expected expanded frame: %q", expanded)
	}
}
