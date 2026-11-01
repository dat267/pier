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

// flatten collapses wrapped card output so tests can match a phrase across
// line breaks without depending on the wrap points.
func flatten(component tui.Component, width int) string {
	return strings.Join(strings.Fields(renderLines(component, width)), " ")
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
	body := strings.Join([]string{"line 1", "line 2", "line 3", "line 4", "line 5", "line 6", "line 7"}, "\n")
	card := RenderGoalCard(stubTheme{}, GoalCard{Label: "test", Body: body}, false)
	rendered := strings.TrimSpace(renderLines(card.Children[1], 500))
	if !strings.Contains(rendered, "(2 more lines, click or expand for full text)") {
		t.Fatalf("missing hint: %q", rendered)
	}
	if strings.Contains(rendered, "line 6") || strings.Contains(rendered, "line 7") {
		t.Fatalf("lines past the cap shown: %q", rendered)
	}
}

func TestRenderGoalCardShowsCompleteBodyWhenExpanded(t *testing.T) {
	long := strings.Repeat("a", 200)
	card := RenderGoalCard(stubTheme{}, GoalCard{Label: "test", Body: long}, true)
	rendered := joinTrimmed(card.Children[1], 500)
	if rendered != "\n"+long {
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
	if RenderGetGoalRenderResult(nil, stubTheme{}, false) == nil {
		t.Fatal("get_goal nil result")
	}
	g := GoalView{GoalSnapshot: createTestGoal("test", t0, nil), TurnsStarted: 2}
	if RenderGetGoalRenderResult(&g, stubTheme{}, false) == nil {
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
	objective := strings.TrimSpace(strings.Repeat("Create a long goal with every acceptance criterion and verification step.\n", 7))
	collapsed := flatten(RenderSetGoalRenderCall(objective, stubTheme{}, false), 200)
	if !strings.Contains(collapsed, "click or expand") || strings.Contains(collapsed, strings.Join(strings.Fields(objective), " ")) {
		t.Fatalf("collapsed frame: %q", collapsed)
	}
	expanded := flatten(RenderSetGoalRenderCall(objective, stubTheme{}, true), 200)
	if !strings.Contains(expanded, strings.Join(strings.Fields(objective), " ")) {
		t.Fatalf("expanded frame missing objective: %q", expanded)
	}
}

// A create card is multi-line: a title line, then the objective body, so a goal
// call reads like the built-in write card instead of one summary line.
func TestCreateGoalCallFrameIsMultiline(t *testing.T) {
	objective := "build a widget that renders the goal state"
	component := RenderSetGoalRenderCall(objective, stubTheme{}, false)
	lines := component.Render(80)
	if len(lines) < 3 {
		t.Fatalf("expected a title, spacer and objective body; got %q", lines)
	}
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "Set goal") || !strings.Contains(text, objective) {
		t.Fatalf("card = %q", text)
	}
	if strings.Contains(text, "click or expand") {
		t.Fatalf("a short objective must not be truncated: %q", text)
	}
}

func TestUpdateGoalCallFrameExpands(t *testing.T) {
	reason := strings.TrimSpace(strings.Repeat("The external dependency is unavailable and the task cannot proceed.\n", 7))
	collapsed := flatten(RenderSetGoalStatusRenderCall("blocked", reason, stubTheme{}, false), 200)
	if !strings.Contains(collapsed, "click or expand") || strings.Contains(collapsed, strings.Join(strings.Fields(reason), " ")) {
		t.Fatalf("collapsed frame: %q", collapsed)
	}
	expanded := flatten(RenderSetGoalStatusRenderCall("blocked", reason, stubTheme{}, true), 200)
	if !strings.Contains(expanded, strings.Join(strings.Fields(reason), " ")) {
		t.Fatalf("expanded frame missing reason: %q", expanded)
	}
}

// get_goal reports the goal across several lines: state, objective, and the
// budget or blocker detail when present.
func TestGetGoalResultIsMultiline(t *testing.T) {
	budget := &GoalBudget{TokenLimit: new(int64(500))}
	g := GoalView{
		GoalSnapshot: GoalSnapshot{
			ID: "g1", Revision: 3, Objective: "ship the widget", Phase: PhasePaused,
			BlockedReason: &BlockedReason{Code: "human-paused", Message: "Paused by user."},
			Budget:        budget, CreatedAt: t0, UpdatedAt: t0,
		},
		TurnsStarted: 2, UsedTokens: 120,
	}
	lines := RenderGetGoalRenderResult(&g, stubTheme{}, false).Render(80)
	text := flatten(RenderGetGoalRenderResult(&g, stubTheme{}, false), 80)
	for _, want := range []string{
		"paused · rev 3 · 2 rounds",
		"ship the widget",
		"budget: tokens 120/500",
		"reason: human-paused: Paused by user.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("card %q missing %q", text, want)
		}
	}
	if len(lines) < 4 {
		t.Fatalf("expected one line per detail, got %q", lines)
	}
	// A long objective is previewed with the expand hint, and expanding shows it.
	long := strings.TrimSpace(strings.Repeat("objective sentence\n", 20))
	longGoal := GoalView{GoalSnapshot: GoalSnapshot{Revision: 1, Objective: long, Phase: PhaseActive}, Armed: true}
	collapsed := flatten(RenderGetGoalRenderResult(&longGoal, stubTheme{}, false), 80)
	if strings.Contains(collapsed, strings.Join(strings.Fields(long), " ")) || !strings.Contains(collapsed, "click or expand") {
		t.Fatalf("collapsed objective: %q", collapsed)
	}
	expanded := flatten(RenderGetGoalRenderResult(&longGoal, stubTheme{}, true), 80)
	if !strings.Contains(expanded, strings.Join(strings.Fields(long), " ")) {
		t.Fatalf("expanded objective missing: %q", expanded)
	}
}

func TestSetGoalCreateResultReportsOutcome(t *testing.T) {
	created := renderLines(RenderSetGoalCreateResult(SetGoalCreateResult{HasGoal: true, Revision: 2}, stubTheme{}), 120)
	if !strings.Contains(created, "Goal created") || !strings.Contains(created, "rev 2") {
		t.Fatalf("created card = %q", created)
	}
	replaced := renderLines(RenderSetGoalCreateResult(SetGoalCreateResult{Replaced: true, HasGoal: true, Revision: 1}, stubTheme{}), 120)
	if !strings.Contains(replaced, "Goal replaced") {
		t.Fatalf("replaced card = %q", replaced)
	}
	errCard := renderLines(RenderSetGoalCreateResult(SetGoalCreateResult{IsError: true, Text: "Provide an objective"}, stubTheme{}), 120)
	if !strings.Contains(errCard, "Provide an objective") {
		t.Fatalf("error card = %q", errCard)
	}
}

func TestClickToExpandTogglesOnLeftClick(t *testing.T) {
	objective := strings.TrimSpace(strings.Repeat("complete the goal objective with all acceptance criteria.\n", 7))
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
	if !strings.Contains(strings.Join(strings.Fields(expanded), " "), strings.Join(strings.Fields(objective), " ")) {
		t.Fatalf("expected expanded frame: %q", expanded)
	}
}
