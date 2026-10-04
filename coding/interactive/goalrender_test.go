package interactive

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/goal"
)

// TestGoalEntryRendererRendersCards pins the transcript renderer wiring: a
// durable lifecycle entry and a round entry resolve to card components.
func TestGoalEntryRendererRendersCards(t *testing.T) {
	newRendererTestTheme(t)
	change, _ := json.Marshal(goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal:      &goal.GoalSnapshot{ID: "g1", Revision: 1, Objective: "obj", Phase: goal.PhaseActive, CreatedAt: 1, UpdatedAt: 1},
		Timestamp: 1,
	})
	renderer := GoalEntryRenderer(goal.GoalCustomType)
	if renderer == nil {
		t.Fatal("no renderer for pi-goal")
	}
	if component := renderer(CustomEntry{CustomType: goal.GoalCustomType, Data: json.RawMessage(change)}, EntryRenderOptions{}, ActiveTheme()); component == nil {
		t.Fatal("create entry rendered nil")
	}

	turn, _ := json.Marshal(goal.GoalTurnEntry{GoalID: "g1", Revision: 1, Turn: 3, Timestamp: 1})
	turnRenderer := GoalEntryRenderer(goal.GoalTurnType)
	if turnRenderer == nil {
		t.Fatal("no renderer for pi-goal-turn")
	}
	component := turnRenderer(CustomEntry{CustomType: goal.GoalTurnType, Data: json.RawMessage(turn)}, EntryRenderOptions{}, ActiveTheme())
	if component == nil {
		t.Fatal("turn entry rendered nil")
	}
	if lines := component.Render(80); len(lines) != 1 {
		t.Fatalf("turn entry lines = %d", len(lines))
	}

	if GoalEntryRenderer("pi-other") != nil {
		t.Fatal("unknown custom type resolved a renderer")
	}
}
