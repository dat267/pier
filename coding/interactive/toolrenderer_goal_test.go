package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/goal"
)

func goalViewForTest(objective string, phase goal.GoalPhase, turns int) *goal.GoalView {
	return &goal.GoalView{
		ID:           "goal-test",
		Revision:     2,
		Objective:    objective,
		Phase:        phase,
		CreatedAt:    1,
		UpdatedAt:    2,
		Armed:        true,
		TurnsStarted: turns,
	}
}

func TestGoalToolRenderers(t *testing.T) {
	theme := newRendererTestTheme(t)

	// get_goal call card.
	if got := strings.TrimSpace(renderCall("get_goal", json.RawMessage(`{}`), theme)); got != "Get goal" {
		t.Fatalf("get_goal call = %q", got)
	}
	// get_goal result, with a goal: state, then the objective body.
	details, err := json.Marshal(map[string]any{"goal": goalViewForTest("ship it", goal.PhaseActive, 2)})
	if err != nil {
		t.Fatal(err)
	}
	result := &SortToolResultContent{Details: details}
	got := renderResult("get_goal", result, false, theme)
	for _, want := range []string{"active", "rev 2", "2 rounds", "ship it"} {
		if !strings.Contains(got, want) {
			t.Fatalf("get_goal result = %q, want %q", got, want)
		}
	}
	if lines := strings.Split(strings.TrimRight(got, " \n"), "\n"); len(lines) < 2 {
		t.Fatalf("get_goal result must be multi-line: %q", got)
	}
	// get_goal result with no goal.
	if got := renderResult("get_goal", &SortToolResultContent{Details: json.RawMessage(`{"goal":null}`)}, false, theme); !strings.Contains(got, "No goal set") {
		t.Fatalf("get_goal empty result = %q", got)
	}

	// set_goal call card, objective: a title line and the objective body.
	if got := renderCall("set_goal", json.RawMessage(`{"objective":"build a widget"}`), theme); !strings.Contains(got, "Set goal") || !strings.Contains(got, "build a widget") {
		t.Fatalf("set_goal objective call = %q", got)
	}

	// set_goal call card, status.
	if got := renderCall("set_goal", json.RawMessage(`{"status":"complete"}`), theme); !strings.Contains(got, "Set goal → complete") {
		t.Fatalf("set_goal status call = %q", got)
	}
	// set_goal create result: a compact outcome line, not the raw reply.
	createDetails, err := json.Marshal(map[string]any{"goal": map[string]any{"id": "g1", "revision": 1, "phase": "active"}})
	if err != nil {
		t.Fatal(err)
	}
	createResult := &SortToolResultContent{
		Details: createDetails,
		Content: []ToolResultContent{{Type: "text", Text: "Goal created (id g1, revision 1)."}},
	}
	if got := renderResult("set_goal", createResult, false, theme); !strings.Contains(got, "Goal created") || !strings.Contains(got, "rev 1") {
		t.Fatalf("set_goal create result = %q", got)
	}
	replacedResult := &SortToolResultContent{
		Details: createDetails,
		Content: []ToolResultContent{{Type: "text", Text: "Goal replaced (id g1, revision 1)."}},
	}
	if got := renderResult("set_goal", replacedResult, false, theme); !strings.Contains(got, "Goal replaced") {
		t.Fatalf("set_goal replace result = %q", got)
	}

	// set_goal result, phase complete.
	details2, err := json.Marshal(map[string]any{"goal": map[string]any{"phase": "complete"}, "action": "complete"})
	if err != nil {
		t.Fatal(err)
	}
	result2 := &SortToolResultContent{IsError: false, Details: details2}
	if got := renderResult("set_goal", result2, false, theme); !strings.Contains(got, "Goal complete") {
		t.Fatalf("set_goal result = %q", got)
	}
	// set_goal result, status clear (D232): the goal is gone, so the card
	// must not fall back to "Goal complete".
	detailsClear, err := json.Marshal(map[string]any{"goal": nil, "action": "clear"})
	if err != nil {
		t.Fatal(err)
	}
	resultClear := &SortToolResultContent{IsError: false, Details: detailsClear}
	if got := renderResult("set_goal", resultClear, false, theme); !strings.Contains(got, "Goal cleared") {
		t.Fatalf("set_goal clear result = %q", got)
	}
	// set_goal error result.
	result3 := &SortToolResultContent{
		IsError: true,
		Content: []ToolResultContent{{Type: "text", Text: "Provide an objective to set the goal or a status to end it."}},
	}
	if got := renderResult("set_goal", result3, false, theme); !strings.Contains(got, "Provide an objective") {
		t.Fatalf("set_goal error result = %q", got)
	}
}

// The rendered card, not just the renderer output: a goal create shows the
// title, the objective body and the outcome line, so the box is multi-line like
// the built-in tool cards.
func TestGoalCreateCardRendersMultipleLines(t *testing.T) {
	if testing.Short() {
		t.Skip("renders a full tool card")
	}
	_ = newRendererTestTheme(t)
	objective := "Ship the widget renderer with multiline goal boxes"
	card := NewToolExecutionComponent("set_goal", "call-1",
		json.RawMessage(`{"objective":"`+objective+`"}`),
		ToolExecutionOptions{}, WithBuiltInRenderers("set_goal", nil), nil, "/tmp/proj")
	details, err := json.Marshal(map[string]any{"goal": map[string]any{"id": "g1", "revision": 1, "phase": "active"}})
	if err != nil {
		t.Fatal(err)
	}
	card.UpdateResult(&SortToolResultContent{
		Details: details,
		Content: []ToolResultContent{{Type: "text", Text: "Goal created (id g1, revision 1)."}},
	}, false)
	lines := card.Render(80)
	rendered := coding.StripAnsi(strings.Join(lines, "\n"))
	for _, want := range []string{"Set goal", objective, "Goal created ✓", "rev 1"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("card missing %q:\n%s", want, rendered)
		}
	}
	if len(lines) < 5 {
		t.Fatalf("card is %d lines, want a multi-line box:\n%s", len(lines), rendered)
	}
}
