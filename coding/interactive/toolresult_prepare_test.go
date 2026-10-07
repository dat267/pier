package interactive

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

func TestDetachedToolPreparationRemainsSynchronous(t *testing.T) {
	InitTheme("dark", false)
	c := NewToolExecutionComponent("bash", "call", map[string]any{"command": "output"}, ToolExecutionOptions{}, WithBuiltInRenderers("bash", nil), nil, "/tmp")
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(func() func()) bool { t.Error("detached warming submitted nested work"); return false }})
	c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("output\n", 10000)}}}, false)
	preparer, ok := any(c).(tui.Preparer)
	if !ok {
		t.Fatal("detached tool has no synchronous Prepare")
	}
	preparer.Prepare(80)
	if strings.Contains(strings.Join(c.Render(80), "\n"), "Preparing tool output...") {
		t.Fatal("detached warming left pending result")
	}
}

func TestDisablingToolPreparationRejectsOutstandingWork(t *testing.T) {
	InitTheme("dark", false)
	c := NewToolExecutionComponent("read", "call", map[string]any{"path": "output.txt"}, ToolExecutionOptions{}, WithBuiltInRenderers("read", nil), nil, "/tmp")
	var work func() func()
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
	c.SetExpanded(true)
	c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("output\n", 10000)}}}, false)
	c.Render(80)
	apply := work()
	c.SetResultPreparation(nil)
	if strings.Contains(strings.Join(c.Render(80), "\n"), "Preparing tool output...") {
		t.Fatal("disabling backend kept pending display")
	}
	apply()
}

// The owner must instead retain a complete display while private work runs.
func TestLargeReadResultPreparesOffLoop(t *testing.T) {
	InitTheme("dark", false)
	c := NewToolExecutionComponent("read", "call", map[string]any{"path": "output.txt"}, ToolExecutionOptions{}, WithBuiltInRenderers("read", nil), nil, "/tmp")
	setter, ok := any(c).(interface {
		SetResultPreparation(*tui.MarkdownPreparation)
	})
	if !ok {
		t.Fatal("tool result has no optional preparation backend")
	}
	var work func() func()
	setter.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
	c.SetExpanded(true)
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("plain output\n", 6000)}}}
	c.UpdateResult(result, false)
	pending := c.Render(80)
	if work == nil {
		t.Fatal("large expanded output ran inline")
	}
	if !strings.Contains(strings.Join(pending, "\n"), "Preparing tool output...") {
		t.Fatal("cold result missing pending label")
	}
	apply := work()
	if !reflect.DeepEqual(c.Render(80), pending) {
		t.Fatal("worker mutated attached display")
	}
	apply()
	sync := NewToolExecutionComponent("read", "call", map[string]any{"path": "output.txt"}, ToolExecutionOptions{}, &readRenderers, nil, "/tmp")
	sync.SetExpanded(true)
	sync.UpdateResult(result, false)
	if !reflect.DeepEqual(c.Render(80), sync.Render(80)) {
		t.Fatal("prepared output differs from synchronous rendering")
	}
}
