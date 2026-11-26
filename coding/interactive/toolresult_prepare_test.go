package interactive

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
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

// D205: only a large, text-only result of a known built-in renderer may be
// captured, and its details must be one of the known shapes within the size
// cap; everything else keeps rendering on the owner with live state.
func TestBuiltinResultCaptureKeepsOnlyLargeKnownValues(t *testing.T) {
	theme := newRendererTestTheme(t)
	largeText := []ToolResultContent{{Type: "text", Text: strings.Repeat("plain output\n", 6000)}}
	prepare := func(tool string, expanded bool) *ToolExecutionComponent {
		c := NewToolExecutionComponent(tool, "call", map[string]any{"path": "output.txt", "command": "ls"}, ToolExecutionOptions{}, WithBuiltInRenderers(tool, nil), nil, "/tmp")
		c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(func() func()) bool { return true }})
		c.SetExpanded(expanded)
		return c
	}
	check := func(name string, component *ToolExecutionComponent, result *SortToolResultContent, want bool) {
		t.Helper()
		component.UpdateResult(result, false)
		if got := component.captureBuiltinResult(theme, &ToolRenderContext{}) != nil; got != want {
			t.Fatalf("%s: captured = %v, want %v", name, got, want)
		}
	}
	small := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: "small"}}}
	check("small result", prepare("bash", true), small, false)
	withImage := &SortToolResultContent{Content: append([]ToolResultContent{{Type: "image", Data: "AA==", MimeType: "image/png"}}, largeText...)}
	check("image block", prepare("bash", true), withImage, false)
	largeDetails := &SortToolResultContent{Content: largeText, Details: json.RawMessage(`"` + strings.Repeat("x", 20*1024) + `"`)}
	check("oversized details", prepare("bash", true), largeDetails, false)
	smallDetails := &SortToolResultContent{Content: largeText, Details: json.RawMessage(`{"ok":true}`)}
	check("known raw details", prepare("bash", true), smallDetails, true)
	unknown := &SortToolResultContent{Content: largeText, Details: struct{ Unknown int }{Unknown: 1}}
	check("unknown details", prepare("bash", true), unknown, false)
	check("known bash details", prepare("bash", true), &SortToolResultContent{Content: largeText, Details: &coding.BashToolDetails{}}, true)
	check("collapsed read", prepare("read", false), &SortToolResultContent{Content: largeText, Details: &coding.ReadToolDetails{}}, false)
	check("expanded read", prepare("read", true), &SortToolResultContent{Content: largeText, Details: &coding.ReadToolDetails{}}, true)
	check("read value details", prepare("read", true), &SortToolResultContent{Content: largeText, Details: coding.ReadToolDetails{}}, false)
	disabled := NewToolExecutionComponent("bash", "call", map[string]any{"command": "ls"}, ToolExecutionOptions{}, WithBuiltInRenderers("bash", nil), nil, "/tmp")
	check("disabled frames", disabled, &SortToolResultContent{Content: largeText}, false)
	// A displaced call keeps its ordinary frame and streams its output, so its
	// large result still takes the capture path: the handle is a status, not a
	// reason to render nothing at all. It reaches here only if a background handle
	// is not special-cased ahead of the capture (D538).
	displaced := prepare("bash", true)
	displaced.MarkBackgrounded("bg-1")
	check("displaced call", displaced, &SortToolResultContent{
		Content: largeText,
		Details: json.RawMessage(`{"backgroundHandle":"bg-1","outputPath":"/tmp/x.log"}`),
	}, true)
}
