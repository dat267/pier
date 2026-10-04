package interactive

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

func skipTestDefinition() *ToolRenderers {
	return &ToolRenderers{
		RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
			return tui.NewText("call", 0, 0, nil)
		},
		RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
			text := ""
			for _, content := range result.Content {
				if content.Type == "text" {
					text = content.Text
				}
			}
			return tui.NewText(text, 0, 0, nil)
		},
	}
}

func skipTextResult(text string) *SortToolResultContent {
	return &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: text}}}
}

// TestToolExecutionUpdateReachesSkippingParent pins the chat's
// SkipUnchangedChildren contract for tool results: updateDisplay must bump the
// tool revision, or the parent serves the previous result.
func TestToolExecutionUpdateReachesSkippingParent(t *testing.T) {
	newRendererTestTheme(t)
	chat := &tui.Container{SkipUnchangedChildren: true}
	component := NewToolExecutionComponent("read", "call-1", map[string]any{"file": "x"}, ToolExecutionOptions{}, skipTestDefinition(), nil, "/tmp")
	component.MarkExecutionStarted()
	component.SetArgsComplete()
	component.UpdateResult(skipTextResult("first result"), false)
	chat.AddChild(component)

	if rendered := strings.Join(chat.Render(80), "\n"); !strings.Contains(rendered, "first result") {
		t.Fatalf("first render = %q", rendered)
	}
	component.UpdateResult(skipTextResult("second result"), false)
	if rendered := strings.Join(chat.Render(80), "\n"); !strings.Contains(rendered, "second result") {
		t.Fatalf("stale tool lines after UpdateResult: %q", rendered)
	}
}

func tickBenchDefinition(output string) *ToolRenderers {
	return &ToolRenderers{
		RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
			return tui.NewText("call", 0, 0, nil)
		},
		RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
			text := ""
			for _, content := range result.Content {
				if content.Type == "text" {
					text = content.Text
				}
			}
			return tui.NewText(text, 0, 0, nil)
		},
	}
}

func BenchmarkToolAnimationTick(b *testing.B) {
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString("some tool output line with several words to wrap around the terminal width\n")
	}
	output := sb.String()
	for _, mode := range []string{"invalidate", "tick"} {
		b.Run(mode, func(b *testing.B) {
			component := NewToolExecutionComponent("read", "call-1", map[string]any{"file": "x"}, ToolExecutionOptions{}, tickBenchDefinition(output), nil, "/tmp")
			component.MarkExecutionStarted()
			component.SetArgsComplete()
			component.UpdateResult(skipTextResult(output), true)
			component.Render(120)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if mode == "tick" {
					component.AnimationTick()
				} else {
					component.Invalidate()
				}
				_ = component.Render(120)
			}
		})
	}
}

func BenchmarkBashToolAnimationTick(b *testing.B) {
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString("some tool output line with several words to wrap around the terminal width\n")
	}
	output := sb.String()
	def := CreateShellRenderers("$")
	for _, mode := range []string{"invalidate", "tick"} {
		b.Run(mode, func(b *testing.B) {
			component := NewToolExecutionComponent("bash", "call-1", map[string]any{"command": "ls"}, ToolExecutionOptions{}, &def, nil, "/tmp")
			component.MarkExecutionStarted()
			component.SetArgsComplete()
			component.UpdateResult(skipTextResult(output), true)
			component.Render(120)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if mode == "tick" {
					component.AnimationTick()
				} else {
					component.Invalidate()
				}
				_ = component.Render(120)
			}
		})
	}
}

// TestBashElapsedLabelUpdatesOnTheNarrowTick pins the D190 correctness half: the
// narrow tick must still make the clock-driven elapsed label update (the result
// Text's wrap cache survives, but the box's version check re-applies the label).
func TestBashElapsedLabelUpdatesOnTheNarrowTick(t *testing.T) {
	newRendererTestTheme(t)
	base := time.Now()
	original := shellNow
	shellNow = func() time.Time { return base }
	defer func() { shellNow = original }()

	def := CreateShellRenderers("$")
	component := NewToolExecutionComponent("bash", "call-1", map[string]any{"command": "ls"}, ToolExecutionOptions{}, &def, nil, "/tmp")
	component.MarkExecutionStarted()
	component.SetArgsComplete()
	component.UpdateResult(skipTextResult("output"), true)
	first := strings.Join(component.Render(120), "\n")

	shellNow = func() time.Time { return base.Add(5 * time.Second) }
	component.AnimationTick()
	second := strings.Join(component.Render(120), "\n")

	if first == second {
		t.Fatal("the narrow tick did not update the elapsed label")
	}
	if !strings.Contains(second, "Elapsed 5.0s") {
		t.Fatalf("elapsed label missing after the tick:\n%s", second)
	}
}
