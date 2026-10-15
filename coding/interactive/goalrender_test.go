package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/internal/goal"
	"github.com/dat267/pier/tui"
)

func TestGoalEntryRendererRejectsUnknownAndMalformed(t *testing.T) {
	if GoalEntryRenderer("pi-other") != nil {
		t.Fatal("unknown custom type must have no renderer")
	}
	renderer := GoalEntryRenderer(goal.GoalCustomType)
	if renderer == nil {
		t.Fatal("pi-goal needs a renderer")
	}
	if component := renderer(CustomEntry{CustomType: goal.GoalCustomType, Data: "not-json"}, EntryRenderOptions{}, newRendererTestTheme(t)); component != nil {
		t.Fatal("malformed data must render nothing")
	}
}

func TestGoalEntryRendererRendersChangeCard(t *testing.T) {
	theme := newRendererTestTheme(t)
	renderer := GoalEntryRenderer(goal.GoalCustomType)
	data, _ := json.Marshal(goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal:      &goal.GoalSnapshot{ID: "g1", Revision: 1, Objective: "ship it", Phase: goal.PhaseActive, CreatedAt: 1, UpdatedAt: 1},
		Timestamp: 1,
	})
	component := renderer(CustomEntry{CustomType: goal.GoalCustomType, Data: data}, EntryRenderOptions{}, theme)
	if component == nil {
		t.Fatal("expected a card")
	}
	rendered := strings.Join(component.Render(120), "\n")
	if !strings.Contains(rendered, "Goal created") || !strings.Contains(rendered, "ship it") {
		t.Fatalf("card = %q", rendered)
	}
}

func TestGoalTurnEntryRendererRendersFlatLine(t *testing.T) {
	theme := newRendererTestTheme(t)
	renderer := GoalEntryRenderer(goal.GoalTurnType)
	data, _ := json.Marshal(goal.GoalTurnEntry{GoalID: "g1", Revision: 1, Turn: 2, Timestamp: 1})
	component := renderer(CustomEntry{CustomType: goal.GoalTurnType, Data: data}, EntryRenderOptions{}, theme)
	if component == nil {
		t.Fatal("expected a turn line")
	}
	if rendered := strings.Join(component.Render(120), "\n"); !strings.Contains(rendered, "#2") {
		t.Fatalf("turn line = %q", rendered)
	}
}

func TestGoalEntryCardTogglesOnLeftClick(t *testing.T) {
	theme := newRendererTestTheme(t)
	renderer := GoalEntryRenderer(goal.GoalCustomType)
	objective := "complete the goal objective with all acceptance criteria and include all necessary verification steps"
	data, _ := json.Marshal(goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal:      &goal.GoalSnapshot{ID: "g1", Revision: 1, Objective: objective, Phase: goal.PhaseActive, CreatedAt: 1, UpdatedAt: 1},
		Timestamp: 1,
	})
	component := renderer(CustomEntry{CustomType: goal.GoalCustomType, Data: data}, EntryRenderOptions{}, theme)
	handler, ok := component.(interface {
		HandleMouse(event tui.TuiMouseEvent) *tui.TuiMouseDispatchResult
	})
	if !ok {
		t.Fatal("goal card must be clickable")
	}
	if collapsed := strings.Join(component.Render(200), "\n"); !strings.Contains(collapsed, "expand for full text") || strings.Contains(collapsed, objective) {
		t.Fatalf("collapsed card = %q", collapsed)
	}
	handled := handler.HandleMouse(tui.TuiMouseEvent{Type: tui.MouseClick, Button: tui.MouseButtonLeft, X: 2, Y: 1})
	if handled == nil || !handled.Handled {
		t.Fatal("left click must be handled")
	}
	if expanded := strings.Join(component.Render(200), "\n"); !strings.Contains(expanded, objective) {
		t.Fatalf("expanded card = %q", expanded)
	}
}
