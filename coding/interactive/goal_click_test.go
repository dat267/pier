package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/goal"
	"github.com/dat267/pier/tui"
)

// D223 click-to-expand must reach the cached transcript, not just the bare
// renderer. Upstream custom-entry.ts supplies the wrapper around renderer
// output, while the Go transcript skips children with unchanged revisions.
func TestGoalEntryClickUpdatesTranscript(t *testing.T) {
	_ = newRendererTestTheme(t)
	objective := strings.Repeat("acceptance criteria ", 12) + "verification finished"
	data, err := json.Marshal(goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal: &goal.GoalSnapshot{ID: "g1", Revision: 1, Phase: goal.PhaseActive,
			Objective: objective},
	})
	if err != nil {
		t.Fatal(err)
	}
	card := NewCustomEntryComponent(CustomEntry{CustomType: goal.GoalCustomType, Data: data}, GoalEntryRenderer(goal.GoalCustomType))
	chat := &tui.Container{SkipUnchangedChildren: true}
	chat.AddChild(card)
	lines := chat.Render(80)
	if strings.Contains(strings.Join(lines, "\n"), "verification finished") {
		t.Fatal("card should start collapsed")
	}
	for _, expanded := range []bool{true, false} {
		result := chat.HandleMouse(tui.TuiMouseEvent{
			Type: tui.MouseClick, Button: tui.MouseButtonLeft,
			X: 2, Y: 2, Width: 80, Height: len(lines),
		})
		if result == nil || !result.Handled {
			t.Fatal("goal click was not handled")
		}
		lines = chat.Render(80)
		got := coding.StripAnsi(strings.Join(lines, "\n"))
		if strings.Contains(got, "verification finished") != expanded {
			t.Fatalf("expanded=%v, transcript did not update after click:\n%s", expanded, got)
		}
	}
}

// Matching control: goal tool cards own their expansion state, unlike durable
// entry renderers. Exercise clicks through the same cached transcript boundary.
func TestGoalToolClicksUpdateTranscript(t *testing.T) {
	_ = newRendererTestTheme(t)
	objective := strings.Repeat("acceptance criteria ", 12) + "verification finished"
	for _, test := range []struct {
		name    string
		args    any
		details any
	}{
		{"set_goal", map[string]any{"objective": objective}, json.RawMessage(`{"goal":{"revision":1,"phase":"active"}}`)},
		{"get_goal", json.RawMessage(`{}`), map[string]any{"goal": goalViewForTest(objective, goal.PhaseActive, 1)}},
		{"set_goal", map[string]any{"status": "blocked", "blocked_reason": objective}, json.RawMessage(`{"action":"blocked","goal":{"phase":"blocked"}}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			card := NewToolExecutionComponent(test.name, "click", test.args,
				ToolExecutionOptions{}, WithBuiltInRenderers(test.name, nil), nil, t.TempDir())
			card.UpdateResult(&SortToolResultContent{Details: test.details}, false)
			chat := &tui.Container{SkipUnchangedChildren: true}
			chat.AddChild(card)
			lines := chat.Render(80)
			if strings.Contains(strings.Join(lines, "\n"), "verification finished") {
				t.Fatal("card should start collapsed")
			}
			for _, expanded := range []bool{true, false} {
				result := chat.HandleMouse(tui.TuiMouseEvent{
					Type: tui.MouseClick, Button: tui.MouseButtonLeft,
					X: 2, Y: 2, Width: 80, Height: len(lines),
				})
				if result == nil || !result.Handled {
					t.Fatal("goal tool click was not handled")
				}
				lines = chat.Render(80)
				got := coding.StripAnsi(strings.Join(lines, "\n"))
				if strings.Contains(got, "verification finished") != expanded {
					t.Fatalf("expanded=%v, transcript did not update after click:\n%s", expanded, got)
				}
			}
		})
	}
}
