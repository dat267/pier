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

	// create_goal call card.
	if got := renderCall("create_goal", json.RawMessage(`{"objective":"build a widget"}`), theme); !strings.Contains(got, "Create goal: build a widget") {
		t.Fatalf("create_goal call = %q", got)
	}

	// update_goal call card.
	if got := renderCall("update_goal", json.RawMessage(`{"action":"complete"}`), theme); !strings.Contains(got, "Update goal → complete") {
		t.Fatalf("update_goal call = %q", got)
	}
	// update_goal result, phase complete.
	details2, err := json.Marshal(map[string]any{"goal": map[string]any{"phase": "complete"}})
	if err != nil {
		t.Fatal(err)
	}
	result2 := &SortToolResultContent{IsError: false, Details: details2}
	if got := renderResult("update_goal", result2, false, theme); !strings.Contains(got, "Goal complete") {
		t.Fatalf("update_goal result = %q", got)
	}
	// update_goal error result.
	result3 := &SortToolResultContent{
		IsError: true,
		Content: []ToolResultContent{{Type: "text", Text: "revision mismatch"}},
	}
	if got := renderResult("update_goal", result3, false, theme); !strings.Contains(got, "revision mismatch") {
		t.Fatalf("update_goal error result = %q", got)
	}
}
