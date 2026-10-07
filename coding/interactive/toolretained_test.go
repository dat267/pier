package interactive

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

func TestPendingShellFrameKeepsElapsedClockLive(t *testing.T) {
	InitTheme("dark", false)
	oldNow := shellNow
	defer func() { shellNow = oldNow }()
	now := int64(1000)
	shellNow = func() time.Time { return time.UnixMilli(now) }
	c := NewToolExecutionComponent("bash", "call", map[string]any{"command": "output"}, ToolExecutionOptions{}, WithBuiltInRenderers("bash", nil), nil, "/tmp")
	var jobs []func() func()
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { jobs = append(jobs, run); return true }})
	c.MarkExecutionStarted()
	c.SetExpanded(true)
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("output\n", 10000)}}}
	c.UpdateResult(result, true)
	c.Render(80)
	jobs[0]()()
	c.Render(80)
	c.UpdateResult(result, true)
	calls := 0
	theme := ActiveTheme().concrete()
	c.contentBox.SetBgFn(func(text string) string { calls++; return theme.Bg("toolPendingBg", text) })
	now = 3000
	c.AnimationTick()
	lines := c.Render(80)
	if !strings.Contains(strings.Join(lines, "\n"), "Elapsed 2.0s") {
		t.Fatal("pending complete frame froze elapsed clock")
	}
	if calls != 0 {
		t.Fatalf("pending timer repainted %d attached background lines", calls)
	}
	parent := tui.NewBox(0, 0, nil)
	parent.AddChild(c)
	parent.Render(80)
	now = 4000
	c.AnimationTick()
	if !strings.Contains(strings.Join(parent.Render(80), "\n"), "Elapsed 3.0s") {
		t.Fatal("parent missed retained clock update")
	}
}

func TestRetainedToolMouseUsesDisplayedWidth(t *testing.T) {
	InitTheme("dark", false)
	c := NewToolExecutionComponent("read", "call", map[string]any{"path": "output.txt"}, ToolExecutionOptions{}, WithBuiltInRenderers("read", nil), nil, "/tmp")
	var jobs []func() func()
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { jobs = append(jobs, run); return true }})
	c.SetExpanded(true)
	c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("output\n", 10000)}}}, false)
	c.Render(80)
	jobs[0]()()
	complete := c.Render(80)
	c.Render(40)
	calls := 0
	theme := ActiveTheme().concrete()
	c.contentBox.SetBgFn(func(text string) string { calls++; return theme.Bg("toolSuccessBg", text) })
	handled := c.HandleMouse(tui.TuiMouseEvent{Type: tui.MouseClick, Button: tui.MouseButtonLeft, X: 2, Y: 4, Width: 40, Height: len(complete)})
	if handled == nil || !handled.Handled {
		t.Fatal("retained result did not handle click")
	}
	if calls != 0 {
		t.Fatalf("mouse dispatch repainted %d lines", calls)
	}
	if strings.Contains(strings.Join(c.Render(40), "\n"), "output\n") {
		t.Fatal("retained result click did not collapse output")
	}
}

func TestRetainedFrameRetriesRejectedAdmissionInSkippingParent(t *testing.T) {
	InitTheme("dark", false)
	c := NewToolExecutionComponent("read", "call", map[string]any{"path": "output.txt"}, ToolExecutionOptions{}, WithBuiltInRenderers("read", nil), nil, "/tmp")
	var work func() func()
	accept := true
	attempts := 0
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool {
		attempts++
		if !accept {
			return false
		}
		work = run
		return true
	}})
	c.SetExpanded(true)
	c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("old output\n", 6000)}}}, false)
	parent := &tui.Container{SkipUnchangedChildren: true}
	parent.AddChild(c)
	parent.Render(80)
	work()()
	complete := slices.Clone(parent.Render(80))
	accept = false
	c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("new output\n", 6000)}}}, false)
	if !reflect.DeepEqual(parent.Render(80), complete) {
		t.Fatal("busy admission lost complete display")
	}
	before := attempts
	accept = true
	if !reflect.DeepEqual(parent.Render(80), complete) || attempts != before+1 {
		t.Fatal("skipping parent did not retry rejected preparation")
	}
	work()()
	if reflect.DeepEqual(parent.Render(80), complete) {
		t.Fatal("skipping parent missed replacement frame")
	}
}

// D207: D205/D206 retain raw output but upstream Box rendering still reapplies
// background/padding after invalidation. Retain the whole completed frame.
func TestPendingToolRetainsCompleteFrameWithoutRepainting(t *testing.T) {
	InitTheme("dark", false)
	for _, change := range []string{"content", "width", "theme"} {
		t.Run(change, func(t *testing.T) {
			c := NewToolExecutionComponent("read", "call", map[string]any{"path": "output.txt"}, ToolExecutionOptions{}, WithBuiltInRenderers("read", nil), nil, "/tmp")
			var jobs []func() func()
			c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { jobs = append(jobs, run); return true }})
			c.SetExpanded(true)
			result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("old output\n", 6000)}}}
			c.UpdateResult(result, false)
			c.Render(80)
			jobs[0]()()
			complete := slices.Clone(c.Render(80))
			width := 80
			switch change {
			case "content":
				c.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("new output\n", 6000)}}}, false)
			case "width":
				width = 40
			case "theme":
				old := CurrentTheme()
				SetThemeInstance(GetThemeByName("light"))
				defer SetThemeInstance(old)
				c.Invalidate()
			}
			calls := 0
			theme := ActiveTheme().concrete()
			c.contentBox.SetBgFn(func(text string) string { calls++; return theme.Bg("toolSuccessBg", text) })
			pending := c.Render(width)
			if len(jobs) != 2 {
				t.Fatal("pending frame did not admit replacement")
			}
			if calls != 0 {
				t.Fatalf("pending owner repainted %d background lines", calls)
			}
			if !reflect.DeepEqual(pending, complete) {
				t.Fatal("pending display changed complete frame")
			}
			apply := jobs[1]()
			if !reflect.DeepEqual(c.Render(width), complete) {
				t.Fatal("worker changed attached frame")
			}
			apply()
			c.Render(width)
			sync := NewToolExecutionComponent("read", "call", map[string]any{"path": "output.txt"}, ToolExecutionOptions{}, WithBuiltInRenderers("read", nil), nil, "/tmp")
			sync.SetExpanded(true)
			sync.UpdateResult(c.result, false)
			if !reflect.DeepEqual(c.Render(width), sync.Render(width)) {
				t.Fatal("replacement frame differs from synchronous output")
			}
		})
	}
}
