package interactive

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

func TestToolFrameLeavesCustomHeaderRenderingOnOwner(t *testing.T) {
	InitTheme("dark", false)
	header := &toolFrameCustomHeader{}
	definition := WithBuiltInRenderers("read", &ToolRenderers{RenderCall: func(any, *Theme, *ToolRenderContext) tui.Component { return header }})
	c := NewToolExecutionComponent("read", "call", nil, ToolExecutionOptions{}, definition, nil, "/tmp")
	var work func() func()
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
	c.SetExpanded(true)
	c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("output\n", 10000)}}}, false)
	c.Render(80)
	if header.calls != 1 {
		t.Fatalf("custom header rendered %d times, want one owner pass", header.calls)
	}
	work()
	if header.calls != 1 {
		t.Fatal("worker invoked custom header renderer")
	}
}

type toolFrameCustomHeader struct{ calls int }

func (c *toolFrameCustomHeader) Render(int) []string { c.calls++; return []string{"custom header"} }
func (*toolFrameCustomHeader) Invalidate()           {}

func TestPreparedShellFrameKeepsElapsedClockOnOwner(t *testing.T) {
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
	now = 2000
	c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("output\n", 10000)}}}, true)
	calls := 0
	theme := ActiveTheme().concrete()
	c.contentBox.SetBgFn(func(text string) string { calls++; return theme.Bg("toolPendingBg", text) })
	c.Render(80)
	apply := work()
	now = 3000
	calls = 0
	apply()
	lines := strings.Join(c.Render(80), "\n")
	if !strings.Contains(lines, "Elapsed 2.0s") {
		t.Fatal("prepared frame froze elapsed clock")
	}
	if calls > 4 {
		t.Fatalf("elapsed update repainted %d lines, want only sample/tail/padding", calls)
	}
	calls = 0
	now = 4000
	c.AnimationTick()
	if !strings.Contains(strings.Join(c.Render(80), "\n"), "Elapsed 3.0s") {
		t.Fatal("clock stopped after handoff")
	}
	if calls > 3 {
		t.Fatalf("timer repainted %d lines", calls)
	}
}

func TestToolWorkerPreparesCompleteBoxFrame(t *testing.T) {
	InitTheme("dark", false)
	for _, name := range []string{"read", "bash"} {
		t.Run(name, func(t *testing.T) {
			c := NewToolExecutionComponent(name, "call", map[string]any{"path": "output.txt", "command": "output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
			var work func() func()
			c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
			c.SetExpanded(true)
			result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("plain output\n", 6000)}}}
			c.UpdateResult(result, false)
			calls := 0
			theme := ActiveTheme().concrete()
			c.contentBox.SetBgFn(func(text string) string { calls++; return theme.Bg("toolSuccessBg", text) })
			pending := c.Render(80)
			calls = 0
			apply := work()
			if calls != 0 {
				t.Fatal("worker invoked attached background callback")
			}
			if !reflect.DeepEqual(c.Render(80), pending) {
				t.Fatal("worker changed attached frame")
			}
			calls = 0
			apply()
			got := c.Render(80)
			if calls > 1 {
				t.Fatalf("owner repainted %d background lines after worker completed", calls)
			}
			sync := NewToolExecutionComponent(name, "call", map[string]any{"path": "output.txt", "command": "output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
			sync.SetExpanded(true)
			sync.UpdateResult(result, false)
			if !reflect.DeepEqual(got, sync.Render(80)) {
				t.Fatal("prepared full frame differs from synchronous output")
			}
		})
	}
}
