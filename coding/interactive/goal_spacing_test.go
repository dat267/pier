package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/goal"
)

// D223: user-requested goal-card layout. The call and result share the
// tool-execution box (pi 4a080252, components/tool-execution.ts), so verify
// section spacing on the assembled card rather than the individual renderers.
func TestGoalCreateCardSeparatesSections(t *testing.T) {
	_ = newRendererTestTheme(t)
	card := NewToolExecutionComponent("set_goal", "spacing",
		json.RawMessage(`{"objective":"Ship the widget"}`),
		ToolExecutionOptions{}, WithBuiltInRenderers("set_goal", nil), nil, t.TempDir())
	card.UpdateResult(&SortToolResultContent{
		Details: json.RawMessage(`{"goal":{"id":"g1","revision":1,"phase":"active"}}`),
		Content: []ToolResultContent{{Type: "text", Text: "Goal created (id g1, revision 1)."}},
	}, false)
	lines := card.Render(80)
	for i, line := range lines {
		lines[i] = strings.TrimSpace(coding.StripAnsi(line))
	}
	got := strings.Join(lines, "\n")
	want := "Set goal\n\nShip the widget\n\nGoal created ✓ · rev 1"
	if !strings.Contains(got, want) {
		t.Fatalf("goal sections need a blank line between them:\n%s", got)
	}
}

// D223: lifecycle cards use the same title/body separation as the tool card.
func TestGoalEntryCardSeparatesTitleFromObjective(t *testing.T) {
	theme := newRendererTestTheme(t)
	data := json.RawMessage(`{"operation":"create","goal":{"id":"g1","revision":1,"objective":"Ship the widget","phase":"active"}}`)
	card := GoalEntryRenderer(goal.GoalCustomType)(CustomEntry{
		CustomType: goal.GoalCustomType, Data: data,
	}, EntryRenderOptions{Expanded: true}, theme)
	lines := card.Render(80)
	for i, line := range lines {
		lines[i] = strings.TrimSpace(coding.StripAnsi(line))
	}
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "Goal created rev 1\n\nShip the widget") {
		t.Fatalf("goal title needs a blank line before the objective:\n%s", got)
	}
}
