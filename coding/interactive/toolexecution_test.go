package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// tool-execution.ts renders expanded args with JSON.stringify(args, null, 2),
// which does not HTML-escape.
func TestToolExecutionFormatDoesNotHTMLEscapeArgs(t *testing.T) {
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	component := NewToolExecutionComponent("custom", "call-1",
		map[string]any{"text": "a < b & c > d"}, ToolExecutionOptions{},
		WithBuiltInRenderers("custom", nil), nil, "/tmp")
	got := component.formatToolExecution()
	if strings.Contains(got, `\u003c`) || strings.Contains(got, `\u0026`) || strings.Contains(got, `\u003e`) {
		t.Fatalf("formatted execution HTML-escaped: %q", got)
	}
	if !strings.Contains(got, "a < b & c > d") {
		t.Fatalf("formatted execution = %q", got)
	}
}

// pi v1.4.0 tool-execution.ts renders image result blocks inline when the
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

// TestToolExecutionSelfShellRendersImages covers the self shell. The image loop
// runs after both shells are populated and its addChild routes to the self
// container, so a self-shell result's image block renders there too, as the
// reference's render appends its image components inside the self block.
func TestToolExecutionSelfShellRendersImages(t *testing.T) {
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
		RenderShell: "self",
		RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
			return tui.NewText("call", 0, 0, nil)
		},
		RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
			return tui.NewText(GetTextOutput(result, context.ShowImages), 0, 0, nil)
		},
	}
	result := &SortToolResultContent{Content: []ToolResultContent{
		{Type: "text", Text: "result"},
		{Type: "image", Data: "iVBORw0KGgoAAAANSUhEUgAAACgAAAAK", MimeType: "image/png"},
	}}
	component := NewToolExecutionComponent("edit", "call-1", map[string]any{}, ToolExecutionOptions{}, definition, nil, "/tmp")
	component.MarkExecutionStarted()
	component.SetArgsComplete()
	component.UpdateResult(result, false)
	rendered := strings.Join(component.Render(80), "\n")
	if !strings.Contains(rendered, "result") || !strings.Contains(rendered, "\x1b_Ga=T,f=100") {
		t.Fatalf("the self shell must render its content and its image: %q", rendered)
	}
}

// D538: every tool can be moved to the background, not only bash, so the
// user-facing handle line is applied where every frame renders its result. An
// MCP frame printed the model's instructions ("do not wait for it…") as tool
// output.
func TestBackgroundHandleRendersInEveryFrame(t *testing.T) {
	theme := newRendererTestTheme(t)
	result := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "This call is still running after 60 seconds, so it was moved to the background as bg-9.\n\n" +
			"Its output is being written to /tmp/pi-background-m.log. You will be told once, when it finishes.\n\n" +
			"Continue with other work; do not wait for it, and do not call it again."}},
		Details: json.RawMessage(`{"backgroundHandle":"bg-9","toolCallId":"c9","toolName":"slow_search","outputPath":"/tmp/pi-background-m.log"}`),
	}
	component := safeRenderResult(mcpRenderers("slow_search").RenderResult, result, ToolRenderResultOptions{}, theme, &ToolRenderContext{})
	if component == nil {
		t.Fatal("the MCP frame rendered nothing")
	}
	text := stripAnsiEscapes(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(text, "Moved to the background as bg-9") || !strings.Contains(text, "/tmp/pi-background-m.log") {
		t.Fatalf("the MCP frame lacks the handle line:\n%s", text)
	}
	if strings.Contains(text, "do not wait") {
		t.Fatalf("the MCP frame shows the model's instructions:\n%s", text)
	}
	// A tool with no result renderer takes the fallback frame, which shows the
	// same line.
	InitTheme("dark", false)
	fallback := NewToolExecutionComponent("slow_tool", "c9", map[string]any{}, ToolExecutionOptions{},
		WithBuiltInRenderers("slow_tool", nil), nil, "/tmp")
	fallback.result = result
	if got := stripAnsiEscapes(strings.Join(fallback.createResultFallback().Render(120), "\n")); !strings.Contains(got, "Moved to the background as bg-9") || strings.Contains(got, "do not wait") {
		t.Fatalf("the fallback frame shows %q", got)
	}
	// The transcript keeps the model's text: only the rendering changes.
	if !strings.Contains(result.Content[0].Text, "do not wait") {
		t.Fatal("rendering rewrote the stored result")
	}
}
