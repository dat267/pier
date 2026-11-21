package interactive

import (
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// This file is the producer the export page's `renderedTools` map expects
// (export-html/tool-renderer.ts): a tool whose card is drawn by the transcript's
// own renderer, which for this port means an MCP tool. The page's template draws
// the built-in tools itself; an MCP call has no template branch, so without this
// it fell back to raw JSON.

// exportToolWidth is the column width the pre-rendered cards are laid out for
// (export-html/tool-renderer.ts default).
const exportToolWidth = 100

// renderToolHTMLForExport pre-renders every MCP tool call and result on the
// branch, keyed by tool call id. A renderer that panics is skipped, so the page
// falls back to its structured rendering.
func renderToolHTMLForExport(session *coding.AgentSession, theme *Theme, cwd string) map[string]*renderedToolHTML {
	if session == nil || session.Sessions == nil {
		return nil
	}
	renderers := func(name string) *ToolRenderers {
		if builtin := WithBuiltInRenderers(name, nil); builtin != nil {
			return nil // the template renders built-in cards itself
		}
		label := ""
		if definition := session.GetToolDefinition(name); definition != nil {
			label = definition.Label
		}
		return mcpToolRenderersFor(name, label)
	}

	rendered := map[string]*renderedToolHTML{}
	state := map[string]any{}
	argsByCall := map[string]any{}
	for _, entry := range session.Sessions.GetBranch("") {
		if entry.Type != "message" || len(entry.Message) == 0 {
			continue
		}
		message, err := ai.UnmarshalMessage(entry.Message)
		if err != nil {
			continue
		}
		switch typed := message.(type) {
		case *ai.AssistantMessage:
			for _, block := range typed.Content {
				call, ok := block.(ai.ToolCall)
				if !ok {
					continue
				}
				definition := renderers(call.Name)
				if definition == nil || definition.RenderCall == nil {
					continue
				}
				argsByCall[call.ID] = call.Arguments
				context := &ToolRenderContext{
					Args: call.Arguments, ToolCallID: call.ID, State: state[call.ID], Cwd: cwd,
					ExecutionStarted: true, ArgsComplete: true, IsPartial: true,
					Expanded: false, ShowImages: false, OutputPad: 1,
				}
				component := safeRenderCall(definition.RenderCall, call.Arguments, theme, context)
				if component == nil {
					continue
				}
				state[call.ID] = context.State
				result := rendered[call.ID]
				if result == nil {
					result = &renderedToolHTML{}
					rendered[call.ID] = result
				}
				result.CallHTML = AnsiLinesToHTML(trimBlankRenderedLines(component.Render(exportToolWidth)))
			}
		case *ai.ToolResultMessage:
			definition := renderers(typed.ToolName)
			if definition == nil || definition.RenderResult == nil {
				continue
			}
			sortResult := &SortToolResultContent{IsError: typed.IsError, Details: typed.Details}
			for _, block := range typed.Content {
				switch content := block.(type) {
				case ai.TextContent:
					sortResult.Content = append(sortResult.Content, ToolResultContent{Type: "text", Text: content.Text})
				case ai.ImageContent:
					sortResult.Content = append(sortResult.Content, ToolResultContent{Type: "image", Data: content.Data, MimeType: content.MimeType})
				}
			}
			collapsed := renderExportResult(definition, sortResult, false, theme, cwd, state[typed.ToolCallID], typed.ToolCallID)
			expanded := renderExportResult(definition, sortResult, true, theme, cwd, state[typed.ToolCallID], typed.ToolCallID)
			result := rendered[typed.ToolCallID]
			if result == nil {
				result = &renderedToolHTML{}
				rendered[typed.ToolCallID] = result
			}
			result.ResultHTMLExpanded = expanded
			if collapsed != "" && collapsed != expanded {
				result.ResultHTMLCollapsed = collapsed
			}
		}
	}
	if len(rendered) == 0 {
		return nil
	}
	return rendered
}

// renderExportResult renders one result state, returning "" when the renderer
// fails or the component has no content.
func renderExportResult(definition *ToolRenderers, result *SortToolResultContent, expanded bool,
	theme *Theme, cwd string, sharedState any, toolCallID string) string {
	context := &ToolRenderContext{
		ToolCallID: toolCallID, State: sharedState, Cwd: cwd,
		ExecutionStarted: true, ArgsComplete: true, IsPartial: false,
		Expanded: expanded, ShowImages: false, OutputPad: 1,
	}
	component := safeRenderResult(definition.RenderResult, result, ToolRenderResultOptions{Expanded: expanded}, theme, context)
	if component == nil {
		return ""
	}
	return AnsiLinesToHTML(trimBlankRenderedLines(component.Render(exportToolWidth)))
}

// trimBlankRenderedLines drops leading and trailing lines that hold nothing but
// escapes (export-html/tool-renderer.ts trimRenderedResultLines).
func trimBlankRenderedLines(lines []string) []string {
	blank := func(line string) bool { return strings.TrimSpace(stripAnsiEscapes(line)) == "" }
	start, end := 0, len(lines)
	for start < end && blank(lines[start]) {
		start++
	}
	for end > start && blank(lines[end-1]) {
		end--
	}
	return lines[start:end]
}

// exportRenderTheme returns the applied theme, or nil when none is initialized
// (an SDK host exporting without a TUI): the pre-rendered map is optional, and
// every renderer falls back to the page's structured rendering.
func exportRenderTheme() *Theme {
	defer func() { _ = recover() }()
	return ActiveTheme()
}
