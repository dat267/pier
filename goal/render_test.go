package goal

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

// Mirrors goal/render.test.ts.

type stubTheme struct{}

func (stubTheme) Fg(_ string, text string) string { return text }
func (stubTheme) Bg(_ string, text string) string { return text }
func (stubTheme) Bold(text string) string         { return text }

func cardBody(box *tui.Box) string {
	if len(box.Children) < 2 {
		return ""
	}
	text, ok := box.Children[1].(*tui.Text)
	if !ok {
		return ""
	}
	return text.Text()
}

func cardLabel(box *tui.Box) string {
	if len(box.Children) < 1 {
		return ""
	}
	text, ok := box.Children[0].(*tui.Text)
	if !ok {
		return ""
	}
	return text.Text()
}

func TestDisplayBody(t *testing.T) {
	cases := map[string]string{
		"<goal_round>\nHello\n":                  "Hello",
		"</goal_complete>\nDone":                 "Done",
		"<goal_blocked>\nStuck\n</goal_blocked>": "Stuck",
		"Hello world":                            "Hello world",
	}
	for input, want := range cases {
		if got := DisplayBody(input); got != want {
			t.Fatalf("DisplayBody(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPhaseColor(t *testing.T) {
	want := map[GoalPhase]string{PhaseActive: "success", PhasePaused: "warning", PhaseBlocked: "error", PhaseComplete: "accent"}
	for phase, color := range want {
		if PhaseColor[phase] != color {
			t.Fatalf("phase %s = %q", phase, PhaseColor[phase])
		}
	}
}

func TestRenderGoalCard(t *testing.T) {
	card := RenderGoalCard(stubTheme{}, GoalCard{Label: "test", Body: "hello"}, false)
	if len(card.Children) != 2 {
		t.Fatalf("children = %d", len(card.Children))
	}
	withPhase := RenderGoalCard(stubTheme{}, GoalCard{Label: "test", Body: "hello", Phase: PhaseActive, HasPhase: true}, false)
	if _, ok := withPhase.Children[0].(*tui.Text); !ok {
		t.Fatal("label child is not a Text")
	}
	withDetail := RenderGoalCard(stubTheme{}, GoalCard{Label: "test", Body: "hello", Detail: "rev 1"}, false)
	if !strings.Contains(cardLabel(withDetail), "rev 1") {
		t.Fatalf("label = %q", cardLabel(withDetail))
	}
	long := strings.Repeat("a", 200)
	truncated := RenderGoalCard(stubTheme{}, GoalCard{Label: "test", Body: long}, false)
	if len([]rune(cardBody(truncated))) != 80 {
		t.Fatalf("collapsed body len = %d", len([]rune(cardBody(truncated))))
	}
}

func TestRenderGoalChangeEntry(t *testing.T) {
	goal := CreateGoalState("test objective", time.UnixMilli(testT0))
	if RenderGoalChangeEntry(GoalChangeEntry{Operation: OpCreate, Goal: &goal}, stubTheme{}, false) == nil {
		t.Fatal("create card nil")
	}
	if RenderGoalChangeEntry(GoalChangeEntry{Operation: OpClear, Cleared: &ClearedRef{ID: "g1", Revision: 1}}, stubTheme{}, false) == nil {
		t.Fatal("clear card nil")
	}
	blocked := goal
	blocked.Phase = PhaseBlocked
	blocked.BlockedReason = &BlockedReason{Code: "err", Message: "stuck"}
	card := RenderGoalChangeEntry(GoalChangeEntry{Operation: OpBlock, Goal: &blocked}, stubTheme{}, false)
	if !strings.Contains(cardBody(card), "err: stuck") {
		t.Fatalf("blocked body = %q", cardBody(card))
	}
}

func TestRenderGoalEventMessage(t *testing.T) {
	for _, kind := range []string{"round", "complete", "blocked", "paused", "resumed"} {
		if RenderGoalEventMessage(kind, "body", 1, true, PhaseActive, stubTheme{}, false) == nil {
			t.Fatalf("%s card nil", kind)
		}
	}
	// Wrap-ups collapse to label-only when not expanded.
	for _, kind := range []string{"complete", "blocked"} {
		phase := PhaseComplete
		if kind == "blocked" {
			phase = PhaseBlocked
		}
		collapsed := RenderGoalEventMessage(kind, "The objective text", 0, false, phase, stubTheme{}, false)
		if strings.Contains(cardBody(collapsed), "objective text") {
			t.Fatalf("%s leaked body when collapsed", kind)
		}
		expanded := RenderGoalEventMessage(kind, "The objective text", 0, false, phase, stubTheme{}, true)
		if !strings.Contains(cardBody(expanded), "objective text") {
			t.Fatalf("%s lost body when expanded", kind)
		}
	}
}

func TestRenderGoalTurnEntry(t *testing.T) {
	card := RenderGoalTurnEntry(GoalTurnEntry{GoalID: "g1", Revision: 1, Turn: 3}, stubTheme{}, false)
	out := card.Render(80)
	if len(out) != 1 {
		t.Fatalf("lines = %d, want 1", len(out))
	}
	if !strings.Contains(out[0], "#3") {
		t.Fatalf("line = %q", out[0])
	}
	if !strings.HasPrefix(out[0], " ") {
		t.Fatalf("no left padding: %q", out[0])
	}
}

func TestToolRenderers(t *testing.T) {
	if RenderGetGoalRenderCall(stubTheme{}) == nil {
		t.Fatal("get_goal call nil")
	}
	if got := RenderGetGoalRenderResult(nil, stubTheme{}).Text(); got != "No goal set" {
		t.Fatalf("no goal = %q", got)
	}
	goal := CreateGoalState("test", time.UnixMilli(testT0))
	withTurns := GoalView{GoalSnapshot: goal, TurnsStarted: 2}
	if got := RenderGetGoalRenderResult(&withTurns, stubTheme{}).Text(); !strings.Contains(got, "rev 1") || !strings.Contains(got, "2 rounds") {
		t.Fatalf("goal result = %q", got)
	}
	if RenderCreateGoalRenderCall("test", stubTheme{}) == nil {
		t.Fatal("create call nil")
	}
	if RenderUpdateGoalRenderCall("complete", "", stubTheme{}) == nil {
		t.Fatal("update call nil")
	}
	if got := RenderUpdateGoalRenderResult(UpdateGoalResult{IsError: true, Text: "fail"}, stubTheme{}).Text(); got != "fail" {
		t.Fatalf("error result = %q", got)
	}
	if got := RenderUpdateGoalRenderResult(UpdateGoalResult{Phase: PhaseBlocked, HasPhase: true, Text: "ok"}, stubTheme{}).Text(); got != "Goal blocked ✓" {
		t.Fatalf("blocked result = %q", got)
	}
}
