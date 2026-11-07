package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/mcp/protocol"
)

// Reference: pi v1.5.0 (f9a26365) extensions/mcp/tools.ts createMcpToolRenderers
// renders an MCP call through core/tools/render-utils.ts formatToolCallWithArgs:
// collapsed, the label and its arguments as `key=value` pairs cut to 100
// characters on the header line; expanded, one `key: value` line per argument.
// Pier had no MCP renderers, so the frame fell back to the generic
// formatToolExecution, which dumps the arguments as pretty-printed JSON and
// grows without bound.
func TestMcpToolCallFramesCollapseArguments(t *testing.T) {
	theme := newRendererTestTheme(t)
	args := json.RawMessage(`{"uri":"file:///tmp/x","server":"docs","query":"a","deep":{"k":"v","j":1}}`)

	collapsed := renderMcpCall(t, args, theme, false)
	lines := strings.Split(collapsed, "\n")
	if len(lines) != 1 {
		t.Fatalf("collapsed call = %q, want one header line", collapsed)
	}
	if !strings.HasPrefix(lines[0], "docs/search") {
		t.Errorf("collapsed call = %q, want the server/tool label", lines[0])
	}
	// Upstream's collapsed pairs interpolate JSON.stringify(value): a string
	// value keeps its quotes, an object its compact JSON.
	for _, pair := range []string{`uri="file:///tmp/x"`, `server="docs"`, `query="a"`, `deep={"k":"v","j":1}`} {
		if !strings.Contains(lines[0], pair) {
			t.Errorf("collapsed call = %q, want %s", lines[0], pair)
		}
	}
	if strings.Contains(collapsed, "\n  ") {
		t.Errorf("collapsed call kept the raw pretty JSON:\n%s", collapsed)
	}

	// D397: the header keeps its shape when the frame expands. The reference
	// reflows it (see the differential below), but a click anywhere in the frame
	// toggles expansion, so reflowing moved the row under the pointer.
	expanded := renderMcpCall(t, args, theme, true)
	if expanded != collapsed {
		t.Errorf("expanded call =\n%s\nwant the collapsed header unchanged:\n%s", expanded, collapsed)
	}

	// Argument keys follow Object.entries order: array-index names first,
	// numerically, then the rest in file order. The expected line was produced
	// by the pinned reference implementation of formatToolCallWithArgs.
	ordered := renderMcpCall(t,
		json.RawMessage(`{"uri":"file:///tmp/x","server":"docs","2":"two","10":"ten","query":"a","deep":{"k":"v","j":1}}`),
		theme, false)
	if want := `docs/search 2="two" 10="ten" uri="file:///tmp/x" server="docs" query="a" deep={"k":"v","j":1}`; ordered != want {
		t.Errorf("collapsed call =\n%s\nwant\n%s", ordered, want)
	}

	// A long argument list is cut to 100 characters plus the ellipsis.
	long := renderMcpCall(t, json.RawMessage(`{"q":"`+strings.Repeat("x", 400)+`"}`), theme, false)
	if body := strings.TrimPrefix(strings.SplitN(long, "\n", 2)[0], "docs/search "); len(body) != 100 {
		t.Errorf("collapsed preview = %d characters (%q), want 100 including the ellipsis", len(body), body)
	} else if !strings.HasSuffix(body, "...") {
		t.Errorf("collapsed preview = %q, want a trailing ellipsis", body)
	}
}

// Reference: the same renderers preview a result with
// modes/interactive/components/visual-truncate.ts VisualLinePreview
// (OUTPUT_PREVIEW_LINES = 5, keep "start"): five wrapped lines, then
// `... (N more lines, ctrl+o to expand)`. Limiting logical lines instead lets
// one minified JSON line fill the screen. Expanded shows every visual line, and
// the saved full-output path is named while collapsed.
func TestMcpToolResultFramesLimitVisualLines(t *testing.T) {
	theme := newRendererTestTheme(t)

	lines := make([]string, 40)
	for index := range lines {
		lines[index] = "line " + itoa(index)
	}
	result := textResult(strings.Join(lines, "\n"))
	collapsed := renderMcpResult(t, result, false, theme)
	rendered := strings.Split(collapsed, "\n")
	// The shell's spacer, the preview and the hint.
	if len(rendered) != mcpOutputPreviewLines+2 {
		t.Fatalf("collapsed result = %d lines, want %d plus the spacer and hint:\n%s",
			len(rendered), mcpOutputPreviewLines, collapsed)
	}
	if rendered[1] != "line 0" {
		t.Errorf("collapsed result starts at %q, want the first line", rendered[1])
	}
	if !strings.Contains(rendered[len(rendered)-1], "... (35 more lines,") ||
		!strings.Contains(rendered[len(rendered)-1], "to expand") {
		t.Errorf("hint = %q, want 35 more lines and the expand hint", rendered[len(rendered)-1])
	}

	// One long JSON line fills the screen when logical lines are counted.
	single := strings.Repeat(`{"a":1,"b":2},`, 40)
	oneLine := renderMcpResult(t, textResult(single), false, theme)
	if got := len(strings.Split(oneLine, "\n")); got > mcpOutputPreviewLines+2 {
		t.Errorf("one long line rendered %d lines, want the visual preview", got)
	}

	expanded := renderMcpResult(t, result, true, theme)
	if got := len(strings.Split(expanded, "\n")); got < len(lines) {
		t.Errorf("expanded result = %d lines, want all %d", got, len(lines))
	}

	full := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "short"}},
		Details: json.RawMessage(`{"server":"docs","tool":"search","fullOutputPath":"/tmp/pi-mcp-abc.txt"}`),
	}
	if got := renderMcpResult(t, full, false, theme); !strings.Contains(got, "Full output: /tmp/pi-mcp-abc.txt") {
		t.Errorf("collapsed result without the saved path:\n%s", got)
	}
	if got := renderMcpResult(t, full, true, theme); strings.Contains(got, "Full output:") {
		t.Errorf("expanded result repeated the saved path:\n%s", got)
	}

	// An error keeps the error color and an empty result adds nothing.
	if got := renderMcpResult(t, &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "boom"}}, IsError: true,
	}, false, theme); !strings.Contains(got, "boom") {
		t.Errorf("error result = %q", got)
	}
	if got := renderMcpResult(t, textResult(""), false, theme); strings.TrimSpace(got) != "" {
		t.Errorf("empty result = %q, want nothing", got)
	}
}

func renderMcpCall(t *testing.T, args any, theme *Theme, expanded bool) string {
	t.Helper()
	renderers := mcpRenderers("docs/search")
	component := renderers.RenderCall(args, theme, &ToolRenderContext{Cwd: "/tmp/proj", Expanded: expanded})
	return trimRenderedRight(component.Render(120))
}

func renderMcpResult(t *testing.T, result *SortToolResultContent, expanded bool, theme *Theme) string {
	t.Helper()
	renderers := mcpRenderers("docs/search")
	component := renderers.RenderResult(result, ToolRenderResultOptions{Expanded: expanded}, theme,
		&ToolRenderContext{Cwd: "/tmp/proj", ShowImages: false, IsError: result.IsError})
	return trimRenderedRight(component.Render(120))
}

// trimRenderedRight strips the theme's padding to the render width, so the
// assertions compare the content rather than the background fill.
func trimRenderedRight(lines []string) string {
	trimmed := make([]string, len(lines))
	for index, line := range lines {
		trimmed[index] = strings.TrimRight(coding.StripAnsi(line), " ")
	}
	return strings.Join(trimmed, "\n")
}

// TestMcpToolFramesReachTheTranscript covers the wiring: an MCP tool resolved
// through the app's session adapter renders with the MCP renderers instead of
// the raw-JSON fallback (upstream registers them with the tool definition).
func TestMcpToolFramesReachTheTranscript(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	label := "docs/search"
	app.session.AttachExtraTools([]agent.AgentTool{{
		Name: "mcp__docs__search", Label: label, Description: "search",
		Parameters: json.RawMessage(`{"type":"object"}`),
	}})
	renderers := app.session.GetToolRenderers("mcp__docs__search")
	if renderers == nil || renderers.RenderCall == nil || renderers.RenderResult == nil {
		t.Fatalf("MCP tool renderers = %+v, want the MCP call and result renderers", renderers)
	}
	args := json.RawMessage(`{"uri":"file:///tmp/x","server":"docs"}`)
	call := renderers.RenderCall(args, ActiveTheme(), &ToolRenderContext{Cwd: "/tmp"})
	rendered := trimRenderedRight(call.Render(100))
	if !strings.HasPrefix(rendered, label+" ") || strings.Contains(rendered, "{") {
		t.Errorf("MCP call frame = %q, want the collapsed label line", rendered)
	}

	// A non-MCP tool with no built-in renderers still falls back.
	if got := app.session.GetToolRenderers("some_other_tool"); got != nil {
		t.Errorf("unknown tool renderers = %+v, want the fallback", got)
	}
}

// TestMcpCallFrameUsesTheServerToolLabelFromARegisteredTool covers the label
// lookup end to end: the tool registered by CreateMcpToolDefinition carries
// `server/tool`, and the frame shows that instead of the sanitized tool name.
func TestMcpCallFrameUsesTheServerToolLabelFromARegisteredTool(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	tool := coding.CreateMcpToolDefinition(coding.McpToolDefinitionOptions{
		Server: "docs-server", Tool: protocol.Tool{Name: "search.tool"}, Name: "mcp__docs_server__search_tool",
	})
	app.session.AttachExtraTools([]agent.AgentTool{tool})
	renderers := app.session.GetToolRenderers(tool.Name)
	if renderers == nil || renderers.RenderCall == nil {
		t.Fatalf("no renderers for the registered MCP tool %q", tool.Name)
	}
	call := renderers.RenderCall(json.RawMessage(`{"server":"docs-server","uri":"file:///a"}`),
		ActiveTheme(), &ToolRenderContext{Cwd: "/tmp"})
	rendered := trimRenderedRight(call.Render(100))
	if !strings.HasPrefix(rendered, "docs-server/search.tool ") {
		t.Errorf("MCP call frame = %q, want the registered server/tool label", rendered)
	}
}

// TestMcpCallHeaderStaysUnreflowedWhenExpanded is the D397 differential: the
// reference rebuilds the header through formatToolCallWithArgs with
// expanded=true, so the one-line `label key=value ...` becomes `label` plus one
// `key: value` line per argument. Pier keeps the one-line form in both states
// (see the row for why), which is the only difference from the reference here.
func TestMcpCallHeaderStaysUnreflowedWhenExpanded(t *testing.T) {
	theme := newRendererTestTheme(t)
	args := json.RawMessage(`{"path":"/tmp/notes.txt","offset":10,"limit":5}`)
	collapsed := renderMcpCall(t, args, theme, false)
	expanded := renderMcpCall(t, args, theme, true)
	if collapsed != expanded {
		t.Fatalf("collapsed = %q, expanded = %q; the header must not reflow", collapsed, expanded)
	}
	if strings.Contains(expanded, "\n") {
		t.Fatalf("expanded header = %q, want one line", expanded)
	}
	if want := `docs/search path="/tmp/notes.txt" offset=10 limit=5`; expanded != want {
		t.Errorf("header = %q, want %q", expanded, want)
	}
}

// TestMcpResultExpansionKeepsThePreviewInPlace is the other half of D397: the
// body grows below the preview lines instead of being replaced, so the rows
// already on screen stay where they were.
func TestMcpResultExpansionKeepsThePreviewInPlace(t *testing.T) {
	theme := newRendererTestTheme(t)
	lines := make([]string, 40)
	for index := range lines {
		lines[index] = "line " + itoa(index)
	}
	result := textResult(strings.Join(lines, "\n"))
	collapsed := strings.Split(renderMcpResult(t, result, false, theme), "\n")
	expanded := strings.Split(renderMcpResult(t, result, true, theme), "\n")
	if len(expanded) <= len(collapsed) {
		t.Fatalf("expanded result = %d lines, want more than collapsed %d", len(expanded), len(collapsed))
	}
	// The shell's spacer line, then the same preview lines at the same rows.
	if expanded[0] != collapsed[0] {
		t.Fatalf("first row moved: collapsed %q, expanded %q", collapsed[0], expanded[0])
	}
	previewRows := mcpOutputPreviewLines
	for index := 1; index <= previewRows; index++ {
		if expanded[index] != collapsed[index] {
			t.Errorf("row %d moved: collapsed %q, expanded %q", index, collapsed[index], expanded[index])
		}
	}
}
