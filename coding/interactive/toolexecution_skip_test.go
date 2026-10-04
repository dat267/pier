package interactive

import (
	"strings"
	"testing"

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
