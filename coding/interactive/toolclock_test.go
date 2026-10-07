package interactive

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

// D208: core/tools/renderers/bash.ts changes one trailing elapsed-label row.
// Accepted pending
// preparation must not copy every retained result row for each clock tick.
func TestPendingClockReusesVersionedFrame(t *testing.T) {
	InitTheme("dark", false)
	oldNow := shellNow
	defer func() { shellNow = oldNow }()
	now := int64(1000)
	shellNow = func() time.Time { return time.UnixMilli(now) }
	c := NewToolExecutionComponent("bash", "call", map[string]any{"command": "output"}, ToolExecutionOptions{}, WithBuiltInRenderers("bash", nil), nil, "/tmp")
	var work func() func()
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
	c.MarkExecutionStarted()
	c.SetExpanded(true)
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("output\n", 10000)}}}
	c.UpdateResult(result, true)
	c.Render(80)
	work()()
	c.Render(80)
	c.UpdateResult(result, true)
	before := c.Render(80)
	version, _ := c.RenderVersion()
	now = 3000
	c.AnimationTick()
	after := c.Render(80)
	if &before[0] != &after[0] {
		t.Fatal("accepted pending clock copied complete frame")
	}
	next, has := c.RenderVersion()
	if !has || next == version {
		t.Fatal("in-place clock update lacks render revision")
	}
	if !strings.Contains(strings.Join(after, "\n"), "Elapsed 2.0s") {
		t.Fatal("clock did not update")
	}
	parent := &tui.Container{SkipUnchangedChildren: true}
	parent.AddChild(c)
	parent.Render(80)
	now = 4000
	c.AnimationTick()
	if !strings.Contains(strings.Join(parent.Render(80), "\n"), "Elapsed 3.0s") {
		t.Fatal("skipping parent missed in-place clock update")
	}
}
