package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// pi v1.3.0 tool-execution.ts renders image result blocks inline when the
// terminal supports an image protocol and show-images is on, and falls back to
// the text placeholder otherwise.
func TestToolExecutionRendersInlineImages(t *testing.T) {
	newRendererTestTheme(t)
	previousCaps := tui.GetTerminalCapabilities()
	previousCells := tui.GetCellDimensions()
	t.Cleanup(func() {
		tui.SetTerminalCapabilities(previousCaps)
		tui.SetCellDimensions(previousCells)
		tui.SetImageTranscoder(nil)
	})
	tui.SetTerminalCapabilities(tui.TerminalCapabilities{Images: tui.ImageProtocolKitty})
	tui.SetCellDimensions(tui.CellDimensions{WidthPx: 10, HeightPx: 10})

	definition := &ToolRenderers{
		RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
			return tui.NewText("call", 0, 0, nil)
		},
		RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
			return tui.NewText(GetTextOutput(result, context.ShowImages), 0, 0, nil)
		},
	}
	// A minimal 40x10 PNG header.
	pngData := "iVBORw0KGgoAAAANSUhEUgAAACgAAAAK"
	result := &SortToolResultContent{Content: []ToolResultContent{
		{Type: "text", Text: "result"},
		{Type: "image", Data: pngData, MimeType: "image/png"},
	}}

	component := NewToolExecutionComponent("read", "call-1", map[string]any{}, ToolExecutionOptions{}, definition, nil, "/tmp")
	component.MarkExecutionStarted()
	component.SetArgsComplete()
	component.UpdateResult(result, false)
	rendered := strings.Join(component.Render(80), "\n")
	if !strings.Contains(rendered, "\x1b_Ga=T,f=100") {
		t.Fatalf("no kitty sequence rendered:\n%q", rendered)
	}
	if strings.Contains(rendered, "[Image:") {
		t.Fatalf("fallback rendered alongside the image:\n%q", rendered)
	}

	component.SetShowImages(false)
	rendered = strings.Join(component.Render(80), "\n")
	if strings.Contains(rendered, "\x1b_Ga=T,f=100") {
		t.Fatalf("image rendered with show-images off:\n%q", rendered)
	}
	if !strings.Contains(rendered, "[Image: [image/png] 40x10]") {
		t.Fatalf("fallback missing:\n%q", rendered)
	}
}
