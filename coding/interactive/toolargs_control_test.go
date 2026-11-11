package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/goal"
)

// M64: tool call arguments are model output. The call cards interpolate them
// into display text, so a crafted path, command, pattern, or file body must not
// reach the terminal as a control sequence (D342's boundary, extended to tool
// arguments).
func TestToolCallArgumentsCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	const escape = "\x1b[2J"
	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"bash command", "bash", map[string]any{"command": "echo hi" + escape + " there"}, "echo hi there"},
		{"edit path", "edit", map[string]any{"path": "/tmp/a" + escape + "b", "oldText": "x", "newText": "y"}, "/tmp/ab"},
		{"write path and body", "write", map[string]any{"path": "/tmp/a" + escape, "content": "body" + escape + "text"}, "bodytext"},
		{"read path", "read", map[string]any{"path": "/tmp/a" + escape + "b"}, "/tmp/ab"},
		{"grep pattern", "grep", map[string]any{"pattern": "needle" + escape, "path": "/tmp"}, "needle"},
		{"find path", "find", map[string]any{"pattern": "*.go", "path": "/tmp" + escape}, "/tmp"},
		{"ls path", "ls", map[string]any{"path": "/tmp" + escape}, "/tmp"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			definition, ok := builtinToolRenderers[test.tool]
			if !ok {
				t.Fatalf("no renderer for %s", test.tool)
			}
			component := NewToolExecutionComponent(test.tool, "call-1", test.args, ToolExecutionOptions{}, &definition, nil, "/tmp")
			rendered := strings.Join(component.Render(160), "\n")
			if strings.Contains(rendered, escape) {
				t.Fatalf("control sequence reached output: %q", rendered)
			}
			if text := coding.StripAnsi(rendered); !strings.Contains(strings.Join(strings.Fields(text), " "), test.want) {
				t.Fatalf("argument text lost: want %q in %q", test.want, text)
			}
		})
	}
}

func TestTreeToolCallPreviewCannotEmitTerminalControls(t *testing.T) {
	const escape = "\x1b[2J"
	row := (&TreeList{}).formatToolCall("bash", map[string]any{"command": "echo hi" + escape})
	if strings.Contains(row, escape) {
		t.Fatalf("tree preview kept the control sequence: %q", row)
	}
	if !strings.Contains(row, "echo hi") {
		t.Fatalf("tree preview lost the command: %q", row)
	}
}

// The tool name is model-supplied too: an unknown or crafted name renders in
// the fallback card.
func TestToolNameCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	const escape = "\x1b[2J"
	component := NewToolExecutionComponent("evil"+escape+"name", "call-1", nil, ToolExecutionOptions{}, nil, nil, "/tmp")
	rendered := strings.Join(component.Render(120), "\n")
	if strings.Contains(rendered, escape) {
		t.Fatalf("tool name control reached output: %q", rendered)
	}
	if text := coding.StripAnsi(rendered); !strings.Contains(text, "evilname") {
		t.Fatalf("tool name lost: %q", text)
	}
}

// Stored session text is untrusted too: sessions are shared with other clients,
// so the tree selector must render control sequences as inert text.
func TestTreeEntryTextCannotEmitTerminalControls(t *testing.T) {
	InitTheme("dark", false)
	content, _ := json.Marshal([]map[string]string{{"type": "text", "text": "hello\x1b[2Jworld"}})
	entry := &coding.SessionTreeNode{Entry: coding.SessionEntry{Type: "message", Message: json.RawMessage(`{"role":"assistant","content":` + string(content) + `}`)}}
	got := (&TreeList{}).getEntryDisplayText(&flatNode{Node: entry}, false)
	if strings.Contains(got, "\x1b[2J") {
		t.Fatalf("tree entry control reached output: %q", got)
	}
	if text := coding.StripAnsi(got); !strings.Contains(text, "helloworld") {
		t.Fatalf("tree entry text lost: %q", text)
	}
}

// Compaction and branch summaries are model-generated text stored in the
// session; they render through Markdown and must stay inert.
func TestSessionSummaryCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	const escape = "\x1b[2J"
	compaction := NewCompactionSummaryMessageComponent("summary"+escape+"text", 1000, nil, 0)
	compaction.SetExpanded(true)
	if rendered := strings.Join(compaction.Render(120), "\n"); strings.Contains(rendered, escape) {
		t.Fatalf("compaction summary control reached output: %q", rendered)
	}
	branch := NewBranchSummaryMessageComponent("branch"+escape+"text", nil, 0)
	if rendered := strings.Join(branch.Render(120), "\n"); strings.Contains(rendered, escape) {
		t.Fatalf("branch summary control reached output: %q", rendered)
	}
}

// Goal objectives and blockers are model-generated session text rendered in
// durable cards.
func TestGoalCardCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	const escape = "\x1b[2J"
	card := goal.RenderGoalChangeEntry(goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal: &goal.GoalSnapshot{ID: "g1", Revision: 1, Phase: goal.PhaseActive,
			Objective: "objective" + escape + "text", CreatedAt: 1, UpdatedAt: 1},
		Timestamp: 1,
	}, ActiveTheme(), false)
	if rendered := strings.Join(card.Render(120), "\n"); strings.Contains(rendered, escape) {
		t.Fatalf("goal objective control reached output: %q", rendered)
	}
	result := goal.RenderGetGoalRenderResult(&goal.GoalView{
		GoalSnapshot: goal.GoalSnapshot{ID: "g1", Revision: 1, Phase: goal.PhaseActive,
			Objective: "objective" + escape + "text", CreatedAt: 1, UpdatedAt: 1},
	}, ActiveTheme(), false)
	if rendered := strings.Join(result.Render(120), "\n"); strings.Contains(rendered, escape) {
		t.Fatalf("goal result control reached output: %q", rendered)
	}
}
