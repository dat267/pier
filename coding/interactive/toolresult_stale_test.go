package interactive

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

func TestPersistedToolResultMetadataPreparesOffLoop(t *testing.T) {
	InitTheme("dark", false)
	for _, name := range []string{"read", "bash"} {
		t.Run(name, func(t *testing.T) {
			c := NewToolExecutionComponent(name, "call", map[string]any{"path": "output.txt", "command": "output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
			var work func() func()
			c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
			c.SetExpanded(true)
			result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("output\n", 10000)}}, Details: json.RawMessage(`{"truncation":{"truncated":true,"truncatedBy":"lines","outputLines":3,"totalLines":4},"fullOutputPath":"/tmp/full-output"}`)}
			c.UpdateResult(result, false)
			c.Render(80)
			if work == nil {
				t.Fatal("persisted metadata forced large result onto owner")
			}
			work()()
			sync := NewToolExecutionComponent(name, "call", map[string]any{"path": "output.txt", "command": "output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
			sync.SetExpanded(true)
			sync.UpdateResult(result, false)
			if !reflect.DeepEqual(c.Render(80), sync.Render(80)) {
				t.Fatal("persisted result differs from synchronous output")
			}
		})
	}
}

// D205: core/tools/renderers/read.ts has no asynchronous completion generation.
// Owner changes must reject old work, including a return to a cached width.
func TestToolPreparationRejectsStaleSnapshots(t *testing.T) {
	InitTheme("dark", false)
	for _, change := range []string{"content", "width", "cached width", "theme", "collapse", "empty"} {
		t.Run(change, func(t *testing.T) {
			c := NewToolExecutionComponent("read", "call", map[string]any{"path": "output.go"}, ToolExecutionOptions{}, WithBuiltInRenderers("read", nil), nil, "/tmp")
			var jobs []func() func()
			c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { jobs = append(jobs, run); return true }})
			c.SetExpanded(true)
			result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("old output\n", 6000)}}}
			c.UpdateResult(result, false)
			c.Render(80)
			jobs[0]()()
			resizePending := c.Render(40)
			if len(jobs) != 2 {
				t.Fatal("resize did not submit work")
			}
			stale := jobs[1]()
			width := 40
			switch change {
			case "content":
				c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("new output\n", 6000)}}}, false)
			case "width":
				width = 60
			case "cached width":
				width = 80
			case "theme":
				oldTheme := CurrentTheme()
				SetThemeInstance(GetThemeByName("light"))
				defer SetThemeInstance(oldTheme)
				c.Invalidate()
			case "collapse":
				c.SetExpanded(false)
			case "empty":
				c.UpdateResult(&SortToolResultContent{}, false)
			}
			expected := c.Render(width)
			stale()
			if !reflect.DeepEqual(c.Render(width), expected) {
				t.Fatal("stale completion changed display")
			}
			if change == "content" && !reflect.DeepEqual(expected, resizePending) {
				t.Fatal("pending update did not retain complete output")
			}
		})
	}
}

func TestToolPreparationCapturesResultValues(t *testing.T) {
	InitTheme("dark", false)
	c := NewToolExecutionComponent("read", "call", map[string]any{"path": "output.txt"}, ToolExecutionOptions{}, WithBuiltInRenderers("read", nil), nil, "/tmp")
	var work func() func()
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
	c.SetExpanded(true)
	details := &coding.ReadToolDetails{Truncation: &coding.TruncationResult{Truncated: true, TruncatedBy: "lines", OutputLines: 3, TotalLines: 4}}
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("captured output\n", 5000)}}, Details: details}
	c.UpdateResult(result, false)
	c.Render(80)
	result.Content[0].Text = "mutated output"
	details.Truncation.TotalLines = 999
	work()()
	lines := coding.StripAnsi(strings.Join(c.Render(80), "\n"))
	if !strings.Contains(lines, "captured output") || !strings.Contains(lines, "3 of 4") || strings.Contains(lines, "999") {
		t.Fatal("worker read mutable caller result")
	}
}

func TestCustomToolRendererRemainsOnOwner(t *testing.T) {
	InitTheme("dark", false)
	calls := 0
	definition := WithBuiltInRenderers("read", &ToolRenderers{RenderResult: func(*SortToolResultContent, ToolRenderResultOptions, *Theme, *ToolRenderContext) tui.Component {
		calls++
		return tui.NewText("custom", 0, 0, nil)
	}})
	c := NewToolExecutionComponent("read", "call", nil, ToolExecutionOptions{}, definition, nil, "/tmp")
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(func() func()) bool { t.Error("custom result submitted to worker"); return false }})
	c.SetExpanded(true)
	c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("output\n", 10000)}}}, false)
	if calls != 1 || !strings.Contains(strings.Join(c.Render(80), "\n"), "custom") {
		t.Fatal("custom callback contract changed")
	}
}
