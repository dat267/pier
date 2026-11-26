package interactive

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
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

// D538: the frame of a displaced call keeps pi's shape. The handle's text is
// written for the model ("do not wait for it…"), and the start notice says the
// call is running, so no frame prints a handle line of its own: an MCP frame once
// showed the model's instructions as tool output, and a bash frame showed a note
// about the log file the notice already names.
func TestBackgroundHandleRendersInEveryFrame(t *testing.T) {
	theme := newRendererTestTheme(t)
	result := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "This call is still running after 60 seconds, so it was moved to the background as bg-9.\n\n" +
			"Its output is being written to /tmp/pi-background-m.log. You will be told once, when it finishes.\n\n" +
			"Continue with other work; do not wait for it, and do not call it again."}},
		Details: json.RawMessage(`{"backgroundHandle":"bg-9","toolCallId":"c9","toolName":"slow_search","outputPath":"/tmp/pi-background-m.log"}`),
	}
	component := safeRenderResult(mcpRenderers("slow_search").RenderResult, result, ToolRenderResultOptions{}, theme, &ToolRenderContext{})
	if component != nil {
		text := stripAnsiEscapes(strings.Join(component.Render(120), "\n"))
		for _, unwanted := range []string{"bg-9", "/tmp/pi-background-m.log", "do not wait", "Moved to the background"} {
			if strings.Contains(text, unwanted) {
				t.Fatalf("the MCP frame shows %q:\n%s", unwanted, text)
			}
		}
	}
	// A tool with no result renderer takes the fallback frame, which shows the
	// model's text only because the handle is all the result holds: the live
	// stream replaces it as soon as the call reports progress.
	InitTheme("dark", false)
	fallback := NewToolExecutionComponent("slow_tool", "c9", map[string]any{}, ToolExecutionOptions{},
		WithBuiltInRenderers("slow_tool", nil), nil, "/tmp")
	fallback.result = result
	if got := stripAnsiEscapes(strings.Join(fallback.createResultFallback().Render(120), "\n")); strings.Contains(got, "Moved to the background") {
		t.Fatalf("the fallback frame shows a handle line: %q", got)
	}
	// The transcript keeps the model's text: only the rendering changes.
	if !strings.Contains(result.Content[0].Text, "do not wait") {
		t.Fatal("rendering rewrote the stored result")
	}
}

// A displaced call has not finished, so its frame must keep rendering and
// ticking with the live output instead of freezing. The start notice leaves it
// running; the finish notice hands it the real elapsed time and stops the ticker.
func TestBackgroundHandleFrameKeepsTicking(t *testing.T) {
	newRendererTestTheme(t)
	handle := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "moved to the background"}},
		Details: json.RawMessage(`{"backgroundHandle":"bg-9","toolCallId":"c9","toolName":"bash","outputPath":"/tmp/pi-background-m.log"}`),
	}
	if got := backgroundHandleID(handle); got != "bg-9" {
		t.Fatalf("handle id = %q", got)
	}
	component := NewToolExecutionComponent("bash", "c9", map[string]any{}, ToolExecutionOptions{},
		WithBuiltInRenderers("bash", nil), nil, "/tmp")
	component.MarkExecutionStarted()
	component.SetArgsComplete()
	component.MarkBackgrounded("bg-9")
	if again, _ := component.AnimationFrame(time.Now()); !again {
		t.Fatal("a backgrounded call's frame stopped ticking")
	}
	component.FinalizeBackground(false)
	if again, _ := component.AnimationFrame(time.Now()); again {
		t.Fatal("a finished call's frame kept ticking")
	}
	if component.IsBackgrounded() {
		t.Fatal("the frame still claims a running background call")
	}
}

// A displaced call's time is its whole life. The handle carries no duration, so
// the frame measures its own clock from execution start; recording the
// foreground part would freeze a 91-second call at the 60-second threshold.
func TestBackgroundedFrameTimeIsTheWholeCall(t *testing.T) {
	newRendererTestTheme(t)
	component := NewToolExecutionComponent("bash", "c9", map[string]any{}, ToolExecutionOptions{},
		WithBuiltInRenderers("bash", nil), nil, "/tmp")
	component.MarkExecutionStarted()
	component.SetArgsComplete()
	// The dispatcher must not record a duration for a handle.
	dispatcher := NewEventDispatcher(NewTranscriptRenderer(&tui.Container{}, nil, nil, nil, nil), nil, nil, nil, nil, nil, nil)
	dispatcher.pendingTools["c9"] = component
	dispatcher.handleToolExecutionEnd(&coding.SessionEvent{Agent: &agent.AgentEvent{
		Type: agent.ToolExecutionEnd, ToolCallID: "c9", ToolName: "bash",
		Result: agent.AgentToolResult{
			Content: []ai.Content{ai.TextContent{Text: "moved"}},
			Details: json.RawMessage(`{"backgroundHandle":"bg-9","toolCallId":"c9","toolName":"bash","outputPath":"/tmp/x.log"}`),
		},
		DurationMs: ptrInt64(60000),
	}})
	if component.durationMs != nil {
		t.Fatalf("the handle recorded a %dms duration, so the label would freeze at the foreground part", *component.durationMs)
	}
	if !component.IsBackgrounded() {
		t.Fatal("the frame was not marked backgrounded")
	}
	// A normal call still records its own time (upstream #10549).
	plain := NewToolExecutionComponent("bash", "c8", map[string]any{}, ToolExecutionOptions{},
		WithBuiltInRenderers("bash", nil), nil, "/tmp")
	plain.MarkExecutionStarted()
	dispatcher.pendingTools["c8"] = plain
	dispatcher.handleToolExecutionEnd(&coding.SessionEvent{Agent: &agent.AgentEvent{
		Type: agent.ToolExecutionEnd, ToolCallID: "c8", ToolName: "bash",
		Result:     agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "done"}}},
		DurationMs: ptrInt64(1500),
	}})
	if plain.durationMs == nil || *plain.durationMs != 1500 {
		t.Fatalf("a normal call lost its recorded duration: %v", plain.durationMs)
	}
}

func ptrInt64(v int64) *int64 { return &v }
