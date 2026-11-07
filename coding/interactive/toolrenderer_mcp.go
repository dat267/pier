package interactive

// Port of extensions/mcp/tools.ts createMcpToolRenderers: an MCP call renders
// through the reference's generic header (core/tools/render-utils.ts
// formatToolCallWithArgs), and its result through a visual-line preview
// (modes/interactive/components/visual-truncate.ts).
//
// Before this the interactive layer had no MCP renderers, so both halves fell
// back to ToolExecutionComponent.formatToolExecution: the arguments as
// pretty-printed JSON and every result line. MCP arguments are often large
// objects (a resource URI plus a filter plus a body) and results are often one
// long minified JSON line, so the frame grew without bound.

import (
	"encoding/json"
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// mcpOutputPreviewLines is how many wrapped result lines a collapsed MCP result
// shows (upstream OUTPUT_PREVIEW_LINES).
const mcpOutputPreviewLines = 5

// mcpCollapsedArgsChars is the collapsed header budget (upstream
// COLLAPSED_ARGS_CHARS).
const mcpCollapsedArgsChars = 100

// mcpRenderers builds the renderers for a call labeled `server/tool` (upstream
// createMcpToolRenderers).
func mcpRenderers(label string) ToolRenderers {
	return ToolRenderers{
		RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
			text := toolTextComponent(context)
			expanded := context != nil && context.Expanded
			text.SetText(formatMcpCall(label, args, theme, expanded))
			return text
		},
		RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
			container := toolContainerComponent(context)
			container.Clear()
			showImages := context != nil && context.ShowImages
			output := strings.TrimSpace(GetTextOutput(result, showImages))
			if output == "" {
				return container
			}
			isError := context != nil && context.IsError
			text := formatMcpResult(output, options.Expanded, isError, theme)
			container.AddChild(tui.NewSpacer(1))
			container.AddChild(tui.NewText(text, 0, 0, nil))
			if !options.Expanded {
				if path := mcpFullOutputPath(result); path != "" {
					container.AddChild(tui.NewText(theme.Fg("muted", "Full output: "+path), 0, 0, nil))
				}
			}
			return container
		},
	}
}

// mcpToolRenderersFor resolves the renderers for a tool name, or nil when the
// name is not an MCP tool. The label is the tool's registered `server/tool`
// label, so a renamed or hash-suffixed tool still shows the server and tool it
// calls.
func mcpToolRenderersFor(toolName string, label string) *ToolRenderers {
	if !strings.HasPrefix(toolName, "mcp__") {
		return nil
	}
	if label == "" {
		label = toolName
	}
	renderers := mcpRenderers(label)
	return &renderers
}

// formatMcpCall renders the call header: the label alone while the arguments
// are still streaming in, then `key=value` pairs cut to the budget when
// collapsed, and one `key: value` line per argument when expanded.
func formatMcpCall(label string, args any, theme *Theme, expanded bool) string {
	header := theme.Fg("toolTitle", theme.Bold(tui.SanitizeUntrustedText(label)))
	entries := mcpArgumentEntries(args)
	if len(entries) == 0 {
		return header
	}
	if expanded {
		lines := make([]string, 0, len(entries))
		for _, entry := range entries {
			lines = append(lines, "  "+entry.key+": "+mcpExpandedValue(entry.value))
		}
		return header + "\n" + theme.Fg("muted", strings.Join(lines, "\n"))
	}
	pairs := make([]string, 0, len(entries))
	for _, entry := range entries {
		pairs = append(pairs, entry.key+"="+entry.value)
	}
	preview := strings.Join(pairs, " ")
	if ai.JSLength(preview) > mcpCollapsedArgsChars {
		preview = ai.JSSlice(preview, 0, mcpCollapsedArgsChars-3) + "..."
	}
	return header + " " + theme.Fg("muted", preview)
}

// mcpArgumentEntries lists the call's arguments in JavaScript enumeration
// order (Object.entries: array-index keys first, then file order).
type mcpArgumentEntry struct {
	key   string
	value string
}

// mcpExpandedValue follows upstream's `key: value` line: a string is shown
// raw, anything else as JSON.stringify(value, null, 2). Tabs become three
// spaces and the value's continuation lines are indented under the key.
func mcpExpandedValue(encoded string) string {
	text := encoded
	if len(encoded) > 0 && encoded[0] == '"' {
		var unquoted string
		if err := json.Unmarshal([]byte(encoded), &unquoted); err == nil {
			text = unquoted
		}
	} else if indented, err := ai.MarshalIndentJSON(json.RawMessage(encoded)); err == nil {
		text = string(indented)
	}
	text = NormalizeDisplayText(strings.ReplaceAll(text, "\t", "   "))
	return strings.ReplaceAll(text, "\n", "\n    ")
}

func mcpArgumentEntries(args any) []mcpArgumentEntry {
	raw, ok := mcpArgumentsJSON(args)
	if !ok {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	var entries []mcpArgumentEntry
	var keys []string
	values := map[string]string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return entries
		}
		key, ok := token.(string)
		if !ok {
			return entries
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return entries
		}
		keys = append(keys, key)
		values[key] = string(value)
	}
	for _, key := range coding.JavaScriptObjectKeyOrder(keys) {
		entries = append(entries, mcpArgumentEntry{key: key, value: values[key]})
	}
	return entries
}

// mcpArgumentsJSON returns the call arguments as a JSON object, or ok=false
// when they are absent or not an object.
func mcpArgumentsJSON(args any) (string, bool) {
	switch typed := args.(type) {
	case nil:
		return "", false
	case json.RawMessage:
		return string(typed), true
	case []byte:
		return string(typed), true
	case string:
		return typed, true
	}
	encoded, err := ai.MarshalJSON(args)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

// formatMcpResult styles the result and, collapsed, keeps the first
// mcpOutputPreviewLines wrapped lines plus the expand hint. Counting wrapped
// lines rather than logical ones keeps one long JSON line from filling the
// screen (like bash's preview and upstream's VisualLinePreview).
func formatMcpResult(output string, expanded bool, isError bool, theme *Theme) string {
	color := "toolOutput"
	if isError {
		color = "error"
	}
	lines := strings.Split(strings.ReplaceAll(output, "\t", "   "), "\n")
	styled := make([]string, 0, len(lines))
	for _, line := range lines {
		styled = append(styled, theme.Fg(color, line))
	}
	text := strings.Join(styled, "\n")
	if expanded {
		return text
	}
	preview := TruncateToVisualLinesStart(text, mcpOutputPreviewLines, mcpPreviewWidth, 0)
	joined := strings.Join(preview.VisualLines, "\n")
	if preview.SkippedCount > 0 {
		joined += "\n" + theme.Fg("muted", "... ("+itoa(preview.SkippedCount)+" more lines,") +
			" " + KeyHint("app.tools.expand", "to expand") + theme.Fg("muted", ")")
	}
	return joined
}

// mcpPreviewWidth is the width the visual-line preview measures against. The
// component re-renders through tui.Text at the real width, so the preview is
// only a bound: counting at a typical terminal width keeps the line count
// steady across resizes while still capping a long line.
const mcpPreviewWidth = 80

// TruncateToVisualLinesStart keeps the first maxVisualLines wrapped lines.
// TruncateToVisualLines keeps the last ones (bash's tail preview).
func TruncateToVisualLinesStart(text string, maxVisualLines int, width int, paddingX int) VisualTruncateResult {
	if text == "" {
		return VisualTruncateResult{}
	}
	tempText := tui.NewText(text, paddingX, 0, nil)
	allVisualLines := tempText.Render(width)
	if len(allVisualLines) <= maxVisualLines {
		return VisualTruncateResult{VisualLines: allVisualLines, SkippedCount: 0}
	}
	// JS's slice(0, 0) returns nothing, so a zero limit keeps no line while
	// still reporting how many were hidden (upstream quirk).
	return VisualTruncateResult{
		VisualLines:  allVisualLines[:maxVisualLines],
		SkippedCount: len(allVisualLines) - maxVisualLines,
	}
}

// mcpFullOutputPath reads the saved full-output path from an MCP result's
// details ({server, tool, fullOutputPath}).
func mcpFullOutputPath(result *SortToolResultContent) string {
	if result == nil || result.Details == nil {
		return ""
	}
	var encoded []byte
	switch typed := result.Details.(type) {
	case json.RawMessage:
		encoded = typed
	case []byte:
		encoded = typed
	case string:
		encoded = []byte(typed)
	default:
		marshaled, err := ai.MarshalJSON(result.Details)
		if err != nil {
			return ""
		}
		encoded = marshaled
	}
	var details struct {
		FullOutputPath *string `json:"fullOutputPath"`
	}
	if err := json.Unmarshal(encoded, &details); err != nil || details.FullOutputPath == nil {
		return ""
	}
	return *details.FullOutputPath
}
