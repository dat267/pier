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
	"strconv"
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// mcpOutputPreviewLines is how many wrapped result lines a collapsed MCP result
// shows (upstream OUTPUT_PREVIEW_LINES).
const mcpOutputPreviewLines = 5

// mcpRenderers builds the renderers for a call labeled `server/tool` (upstream
// createMcpToolRenderers).
func mcpRenderers(label string) ToolRenderers {
	return ToolRenderers{
		RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
			// The clock the result's elapsed label runs on (D493); the shell
			// renderers keep theirs the same way.
			state := shellCallStateFor(context)
			if context != nil && context.ExecutionStarted && state.startedAtMS == 0 {
				state.startedAtMS = shellNow().UnixMilli()
				state.endedAtMS = 0
			}
			text := toolTextComponent(context)
			text.SetText(formatMcpCall(label, args, theme))
			// A running call shows its clock under the header: an MCP server need
			// not stream partial results, so the result renderer (which carries the
			// final `Took` line) may not run until the call ends, and the frame
			// would sit without a time. The label recomputes at render time, so the
			// animation tick moves it (D493).
			if context == nil || !context.ExecutionStarted || context.HasResult {
				return text
			}
			box := &tui.Container{}
			box.AddChild(text)
			box.AddChild(&shellElapsedComponent{state: state, theme: theme, partial: true})
			return box
		},
		RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
			state := shellCallStateFor(context)
			if context != nil && context.DurationMs != nil {
				state.recordedMS = context.DurationMs
			}
			if state.startedAtMS != 0 && state.endedAtMS == 0 && (!options.IsPartial || (context != nil && context.IsError)) {
				state.endedAtMS = shellNow().UnixMilli()
			}
			container := toolContainerComponent(context)
			container.Clear()
			showImages := context != nil && context.ShowImages
			output := strings.TrimSpace(GetTextOutput(result, showImages))
			if output == "" {
				return container
			}
			isError := context != nil && context.IsError
			// A truncated result opens with Codex's prose header
			// (`coding.LimitMcpContent`), which reads as tool output. The shell
			// renderer reports the same facts as one bracketed warning line
			// (core/tools/renderers/bash.ts), so an MCP frame does too: the body
			// loses the header and the path, and the card carries both.
			warnings := []string{}
			if path := mcpFullOutputPath(result); path != "" {
				warnings = append(warnings, "Full output: "+path)
			}
			if body, totalLines, shownLines, ok := mcpTruncationHeader(output); ok {
				output = body
				warnings = append(warnings, "Truncated: showing "+itoa(shownLines)+" of "+itoa(totalLines)+" lines")
			}
			container.AddChild(tui.NewSpacer(1))
			if output != "" {
				container.AddChild(&mcpResultComponent{
					text: output, isError: isError, theme: theme, expanded: options.Expanded,
				})
			}
			if len(warnings) > 0 {
				container.AddChild(tui.NewText("\n"+theme.Fg("warning", "["+strings.Join(warnings, ". ")+"]"), 0, 0, nil))
			}
			// D493: the call's elapsed/took line, as the shell and codemode
			// renderers show it; the reference's MCP renderer omits it, so a long
			// call gave no sign of how long it had been running.
			// A result carries the clock while the call runs (a partial result, or the
			// final one); a call with no result yet shows it under the header instead
			// (see RenderCall), so exactly one clock row is on screen.
			if state.startedAtMS != 0 || state.recordedMS != nil {
				container.AddChild(&shellElapsedComponent{state: state, theme: theme, partial: options.IsPartial})
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

// formatMcpCall renders the call header: the label and every argument as a
// `key=value` pair, uncut and in both states, the way formatShellCall shows a
// whole command. The reference cuts the pairs at 100 units and reflows them into
// one `key: value` line per argument when the frame expands
// (core/tools/render-utils.ts formatToolCallWithArgs); a cut argument, an MCP
// call's query above all, could not be read at any frame size, so pier shows it
// whole and never reflows the header (D397).
func formatMcpCall(label string, args any, theme *Theme) string {
	header := theme.Fg("toolTitle", theme.Bold(tui.SanitizeUntrustedText(label)))
	entries := mcpArgumentEntries(args)
	if len(entries) == 0 {
		return header
	}
	pairs := make([]string, 0, len(entries))
	for _, entry := range entries {
		// The key is decoded from the call's JSON, so an escape sequence in it
		// is a real escape byte; the value is re-serialized and JSON-escaped. Both
		// are untrusted text for the terminal (D342, D449).
		pairs = append(pairs, tui.SanitizeUntrustedText(entry.key)+"="+tui.SanitizeUntrustedText(entry.value))
	}
	return header + " " + theme.Fg("muted", strings.Join(pairs, " "))
}

// mcpArgumentEntries lists the call's arguments in JavaScript enumeration
// order (Object.entries: array-index keys first, then file order).
type mcpArgumentEntry struct {
	key string
	// value is JSON.stringify(value), so a string keeps its quotes and an object
	// its compact JSON.
	value string
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
	values := map[string]mcpArgumentEntry{}
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
		values[key] = mcpArgumentEntry{key: key, value: mcpJSONStringify(value)}
	}
	for _, key := range coding.JavaScriptObjectKeyOrder(keys) {
		entries = append(entries, values[key])
	}
	return entries
}

// mcpJSONStringify renders one argument value the way the reference's
// `JSON.stringify(value)` does: the parsed value re-serialized, not the
// provider's original text. Decoding through `any` gives JavaScript number
// semantics (1.0 -> 1, 1e3 -> 1000, an integer past 2^53 in exponent form).
// Unparseable values fall back to their raw text, which is what
// `JSON.stringify` would have received from a decoded JSON body anyway.
// ParseJavaScriptJSONValue keeps nested object key order, so a value like
// `{"k":"v","j":1}` is not re-sorted.
func mcpJSONStringify(raw json.RawMessage) string {
	encoded, ok := ai.ParseJavaScriptJSONValue(raw)
	if !ok {
		return string(raw)
	}
	return string(encoded)
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

// mcpResultComponent renders an MCP result: its styled text, or, while
// collapsed, the expand hint and then the first mcpOutputPreviewLines visual
// lines.
// Both states wrap at the width Render receives (upstream's VisualLinePreview
// measures the same way), so expanding only adds the rows below the preview and
// never re-wraps one that is already on screen.
type mcpResultComponent struct {
	text     string
	isError  bool
	theme    *Theme
	expanded bool

	cachedWidth  int
	cachedLines  []string
	cachedExpand bool
	hasCache     bool
}

func (c *mcpResultComponent) Render(width int) []string {
	if c.hasCache && c.cachedWidth == width && c.cachedExpand == c.expanded {
		return c.cachedLines
	}
	color := "toolOutput"
	if c.isError {
		color = "error"
	}
	rows := strings.Split(c.text, "\n")
	styled := make([]string, 0, len(rows))
	for _, row := range rows {
		styled = append(styled, c.theme.Fg(color, row))
	}
	// Wrap once, at the width the caller gave us, then either show every visual
	// line (expanded) or the first mcpOutputPreviewLines and the hint. Both
	// states therefore wrap identically, so expanding only adds rows below the
	// preview (upstream's VisualLinePreview + Text pair).
	visual := tui.NewText(strings.Join(styled, "\n"), 0, 0, nil).Render(width)
	lines := visual
	if !c.expanded && len(visual) > mcpOutputPreviewLines {
		lines = make([]string, 0, mcpOutputPreviewLines+1)
		// The hint leads the preview so it sits right under the arguments, where
		// it is read, instead of below five rows of output. The reference
		// truncates it to the frame width, so a narrow terminal keeps it on one
		// line instead of overflowing.
		hint := c.theme.Fg("muted", "... ("+itoa(len(visual)-mcpOutputPreviewLines)+" more lines,") +
			" " + KeyHint("app.tools.expand", "to expand") + c.theme.Fg("muted", ")")
		lines = append(lines, tui.TruncateToWidth(hint, width, "...", false))
		lines = append(lines, visual[:mcpOutputPreviewLines]...)
	}
	c.cachedWidth, c.cachedExpand, c.cachedLines, c.hasCache = width, c.expanded, lines, true
	return lines
}

func (c *mcpResultComponent) Invalidate() { c.hasCache = false }

// mcpTruncationHeader splits the header `coding.LimitMcpContent` writes in front
// of a truncated result: the prose warning, the original token count and the
// total line count, then a blank line, then the kept text and the bracketed path
// line. It returns the body, the total line count of the original output, how
// many lines the body kept, and ok=false when the text carries no header.
func mcpTruncationHeader(output string) (body string, totalLines int, shownLines int, ok bool) {
	const prefix = "Warning: truncated output (original token count: "
	if !strings.HasPrefix(output, prefix) {
		return output, 0, 0, false
	}
	rest := output[len(prefix):]
	firstBreak := strings.IndexByte(rest, '\n')
	if firstBreak < 0 {
		return output, 0, 0, false
	}
	linesLine, afterLines, found := strings.Cut(rest[firstBreak+1:], "\n")
	if !found || !strings.HasPrefix(linesLine, "Total output lines: ") {
		return output, 0, 0, false
	}
	total, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(linesLine, "Total output lines: ")))
	if err != nil {
		return output, 0, 0, false
	}
	body = strings.TrimLeft(afterLines, "\n")
	// The trailing `[Full output: path ...]` line belongs to the card.
	if index := strings.LastIndex(body, "\n\n[Full output: "); index >= 0 &&
		strings.HasSuffix(body, "]") {
		body = body[:index]
	} else if index := strings.LastIndex(body, "[Could not save the full output: "); index >= 0 &&
		strings.HasPrefix(body[index:], "[Could not save") {
		body = strings.TrimRight(body[:index], "\n")
	}
	body = strings.TrimSpace(body)
	shown := 0
	if body != "" {
		shown = len(strings.Split(body, "\n"))
	}
	return body, total, shown, true
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
