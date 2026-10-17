package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/internal/goal"
)

func goalViewForTest(objective string, phase goal.GoalPhase, turns int) *goal.GoalView {
	return &goal.GoalView{
		GoalSnapshot: goal.GoalSnapshot{
			ID:        "goal-test",
			Revision:  2,
			Objective: objective,
			Phase:     phase,
			CreatedAt: 1,
			UpdatedAt: 2,
		},
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
	// get_goal result, with a goal.
	details, err := json.Marshal(map[string]any{"goal": goalViewForTest("ship it", goal.PhaseActive, 2)})
	if err != nil {
		t.Fatal(err)
	}
	result := &SortToolResultContent{Details: details}
	if got := renderResult("get_goal", result, false, theme); !strings.Contains(got, "active") || !strings.Contains(got, "rev 2") || !strings.Contains(got, "2 rounds") {
		t.Fatalf("get_goal result = %q", got)
	}
	// get_goal result with no goal.
	if got := renderResult("get_goal", &SortToolResultContent{Details: json.RawMessage(`{"goal":null}`)}, false, theme); !strings.Contains(got, "No goal set") {
		t.Fatalf("get_goal empty result = %q", got)
	}

	// set_goal call card, objective.
	if got := renderCall("set_goal", json.RawMessage(`{"objective":"build a widget"}`), theme); !strings.Contains(got, "Set goal: build a widget") {
		t.Fatalf("set_goal objective call = %q", got)
	}

	// set_goal call card, status.
	if got := renderCall("set_goal", json.RawMessage(`{"status":"complete"}`), theme); !strings.Contains(got, "Set goal → complete") {
		t.Fatalf("set_goal status call = %q", got)
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
