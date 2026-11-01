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

// D223: all goal tool sections use the same spacing. Upstream pi 4a080252
// components/tool-execution.ts mounts call and result components consecutively,
// so the native goal renderers own the blank line at their shared boundary.
func TestGoalToolCardsSeparateEverySection(t *testing.T) {
	_ = newRendererTestTheme(t)
	for _, tc := range []struct {
		name, tool, args, details, result, want string
		isError                                 bool
	}{
		{name: "get", tool: "get_goal", args: `{}`, details: `{"goal":{"revision":1,"objective":"Ship the widget","phase":"active","armed":true}}`,
			want: "Get goal\n\nactive · rev 1 · 0 rounds\n\nShip the widget"},
		{name: "get empty", tool: "get_goal", args: `{}`, details: `{"goal":null}`,
			want: "Get goal\n\nNo goal set"},
		{name: "get details", tool: "get_goal", args: `{}`, details: `{"goal":{"revision":2,"objective":"Ship the widget","phase":"blocked","budget":{"tokenLimit":500},"usedTokens":120,"blockedReason":{"code":"dependency","message":"Waiting on access"}}}`,
			want: "Get goal\n\nblocked · rev 2 · 0 rounds\n\nShip the widget\n\nbudget: tokens 120/500\nreason: dependency: Waiting on access"},
		{name: "create", tool: "set_goal", args: `{"objective":"Ship the widget"}`, details: `{"goal":{"revision":1,"phase":"active"}}`,
			want: "Set goal\n\nShip the widget\n\nGoal created ✓ · rev 1"},
		{name: "replace", tool: "set_goal", args: `{"objective":"Ship the widget"}`, details: `{"goal":{"revision":1,"phase":"active"}}`, result: "Goal replaced (id g1, revision 1).",
			want: "Set goal\n\nShip the widget\n\nGoal replaced ✓ · rev 1"},
		{name: "complete", tool: "set_goal", args: `{"status":"complete"}`, details: `{"action":"complete","goal":{"phase":"complete"}}`,
			want: "Set goal → complete\n\nGoal complete ✓"},
		{name: "blocked", tool: "set_goal", args: `{"status":"blocked","blocked_reason":"Waiting on access"}`, details: `{"action":"blocked","goal":{"phase":"blocked"}}`,
			want: "Set goal → blocked\n\nWaiting on access\n\nGoal blocked ✓"},
		{name: "clear", tool: "set_goal", args: `{"status":"clear"}`, details: `{"action":"clear"}`,
			want: "Set goal → clear\n\nGoal cleared ✓"},
		{name: "status error", tool: "set_goal", args: `{"status":"complete"}`, isError: true, result: "No active goal",
			want: "Set goal → complete\n\nNo active goal"},
		{name: "create error", tool: "set_goal", args: `{"objective":"Ship the widget"}`, isError: true, result: "Could not save goal",
			want: "Set goal\n\nShip the widget\n\nCould not save goal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card := NewToolExecutionComponent(tc.tool, "spacing", json.RawMessage(tc.args),
				ToolExecutionOptions{}, WithBuiltInRenderers(tc.tool, nil), nil, t.TempDir())
			result := &SortToolResultContent{IsError: tc.isError, Content: []ToolResultContent{{Type: "text", Text: tc.result}}}
			if tc.details != "" {
				result.Details = json.RawMessage(tc.details)
			}
			card.UpdateResult(result, false)
			for _, expanded := range []bool{false, true} {
				card.SetExpanded(expanded)
				lines := card.Render(160)
				for i, line := range lines {
					lines[i] = strings.TrimSpace(coding.StripAnsi(line))
				}
				if got := strings.TrimSpace(strings.Join(lines, "\n")); got != tc.want {
					t.Fatalf("expanded=%v sections:\n%q\nwant:\n%q", expanded, got, tc.want)
				}
			}
		})
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
