package interactive

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/mcp/protocol"
	"github.com/dat267/pier/tui"
)

// Reference: pi v1.5.0 (f9a26365) extensions/mcp/tools.ts createMcpToolRenderers
// renders a call through core/tools/render-utils.ts formatToolCallWithArgs:
// collapsed, the label and its arguments as `key=value` pairs cut to 100 units;
// expanded, the label plus one `key: value` line per argument. Pier shows every
// pair uncut and identical in both states, the way formatShellCall shows a whole
// command (D397), because a cut argument (an MCP call's query above all) cannot
// be read at any frame size.
func TestMcpToolCallShowsEveryArgument(t *testing.T) {
	theme := newRendererTestTheme(t)
	args := json.RawMessage(`{"uri":"file:///tmp/x","server":"docs","query":"a","deep":{"k":"v","j":1}}`)

	header := renderMcpCall(t, args, theme)
	lines := strings.Split(header, "\n")
	if len(lines) != 1 {
		t.Fatalf("call header = %q, want one line", header)
	}
	if !strings.HasPrefix(lines[0], "docs/search") {
		t.Errorf("call header = %q, want the server/tool label", lines[0])
	}
	// Each pair interpolates JSON.stringify(value): a string value keeps its
	// quotes, an object its compact JSON.
	for _, pair := range []string{`uri="file:///tmp/x"`, `server="docs"`, `query="a"`, `deep={"k":"v","j":1}`} {
		if !strings.Contains(lines[0], pair) {
			t.Errorf("call header = %q, want %s", lines[0], pair)
		}
	}

	// A long value is never cut; the header wraps instead.
	long := renderMcpCall(t, json.RawMessage(`{"q":"`+strings.Repeat("x", 400)+`"}`), theme)
	if strings.Count(long, "x") != 400 {
		t.Errorf("header kept %d of 400 characters: %q", strings.Count(long, "x"), long)
	}

	// Argument keys follow Object.entries order: array-index names first,
	// numerically, then the rest in file order. The expected line was produced
	// by the pinned reference implementation of formatToolCallWithArgs.
	ordered := renderMcpCall(t,
		json.RawMessage(`{"uri":"file:///tmp/x","server":"docs","2":"two","10":"ten","query":"a","deep":{"k":"v","j":1}}`), theme)
	if want := `docs/search 2="two" 10="ten" uri="file:///tmp/x" server="docs" query="a" deep={"k":"v","j":1}`; ordered != want {
		t.Errorf("call header =\n%s\nwant\n%s", ordered, want)
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
	// The shell's spacer, then the hint (D397: it leads the preview, right under
	// the arguments), then the first preview lines.
	if !strings.Contains(rendered[1], "... (35 more lines,") ||
		!strings.Contains(rendered[1], "to expand") {
		t.Errorf("hint = %q, want 35 more lines and the expand hint", rendered[1])
	}
	if rendered[2] != "line 0" {
		t.Errorf("collapsed preview starts at %q, want the first line", rendered[2])
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

func renderMcpCall(t *testing.T, args any, theme *Theme) string {
	t.Helper()
	renderers := mcpRenderers("docs/search")
	// Both states render the same header; the expanded flag is set so the test
	// would catch a reflow coming back.
	component := renderers.RenderCall(args, theme, &ToolRenderContext{Cwd: "/tmp/proj", Expanded: true})
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

// TestMcpCallHeaderSerializesValuesLikeJSONStringify pins the value text of the
// call header. Reference: pi v1.5.0 (f9a26365) core/tools/render-utils.ts
// formatToolCallWithArgs builds each pair as `${key}=${JSON.stringify(value)}`,
// so the header shows the parsed value re-serialized (1.0 renders as 1,
// "\u0041" as "A", 1e3 as 1000, and an integer beyond 2^53 in exponent form).
// Pier printed the argument's raw JSON text instead, so a provider's spelling
// leaked into the header.
func TestMcpCallHeaderSerializesValuesLikeJSONStringify(t *testing.T) {
	theme := newRendererTestTheme(t)
	cases := []struct {
		args string
		want string
	}{
		{`{"n":1.0,"s":"a\u0041"}`, `docs/search n=1 s="aA"`},
		{`{"n":1e3,"b":true,"z":null}`, `docs/search n=1000 b=true z=null`},
		{`{"nested":{"a":1.0},"arr":[1.0,2]}`, `docs/search nested={"a":1} arr=[1,2]`},
		{`{"big":1234567890123456789012}`, `docs/search big=1.2345678901234568e+21`},
		{`{"neg":-0}`, `docs/search neg=0`},
	}
	for _, testCase := range cases {
		if got := renderMcpCall(t, json.RawMessage(testCase.args), theme); got != testCase.want {
			t.Errorf("header for %s = %q, want %q", testCase.args, got, testCase.want)
		}
	}
}

// TestMcpResultPreviewWrapsAtTheRenderWidth is the reproduction of the reported
// reflow: the collapsed preview was pre-wrapped at a fixed 80 columns, so on a
// wider terminal it broke a line that the expanded view left whole, and on a
// narrower one it broke it differently. Reference: pi v1.5.0
// modes/interactive/components/visual-truncate.ts VisualLinePreview measures
// with the width its render(width) receives, so collapsed and expanded wrap
// identically at every width.
func TestMcpResultPreviewWrapsAtTheRenderWidth(t *testing.T) {
	theme := newRendererTestTheme(t)
	// One long line that fits on a wide terminal and wraps on a narrow one.
	body := "The `/home/dat/repos/pier` directory uses Go modules, with the following main modules: " +
		"/home/dat/repos/pier/go.mod (module github.com/dat267/pier)"
	more := make([]string, 12)
	for index := range more {
		more[index] = "trailing line " + itoa(index)
	}
	result := textResult(body + "\n" + strings.Join(more, "\n"))

	for _, width := range []int{160, 120, 100, 72} {
		collapsed := renderMcpResultAt(t, result, false, theme, width)
		expanded := renderMcpResultAt(t, result, true, theme, width)
		// The preview wraps identically in both states: collapsing adds the hint
		// row above it and drops the rows past the preview.
		if !strings.Contains(collapsed[1], "more lines,") {
			t.Fatalf("width %d: row 1 = %q, want the hint above the preview", width, collapsed[1])
		}
		for index := 0; index < mcpOutputPreviewLines && 2+index < len(collapsed); index++ {
			if collapsed[2+index] != expanded[1+index] {
				t.Fatalf("width %d row %d: collapsed %q, expanded %q; the preview must wrap at the render width",
					width, index, collapsed[2+index], expanded[1+index])
			}
		}
		// And a collapsed preview never exceeds the width.
		for index, row := range collapsed {
			if codingWidth(row) > width {
				t.Errorf("width %d row %d is %d wide: %q", width, index, codingWidth(row), row)
			}
		}
	}
}

// TestMcpResultPreviewHintCountsVisualLines pins the hint's number at the render
// width: counting at a fixed width reported lines the user cannot see.
func TestMcpResultPreviewHintCountsVisualLines(t *testing.T) {
	theme := newRendererTestTheme(t)
	lines := make([]string, 0, 20)
	for index := range 20 {
		lines = append(lines, "line "+itoa(index))
	}
	result := textResult(strings.Join(lines, "\n"))
	narrow := renderMcpResultAt(t, result, false, theme, 40)
	hint := narrow[1]
	if !strings.Contains(hint, "15 more lines") {
		t.Errorf("hint at width 40 = %q, want 15 more lines (20 lines minus the 5 shown)", hint)
	}
}

// renderMcpResultAt renders an MCP result at a given width.
func renderMcpResultAt(t *testing.T, result *SortToolResultContent, expanded bool, theme *Theme, width int) []string {
	t.Helper()
	renderers := mcpRenderers("gopls/go_workspace")
	component := renderers.RenderResult(result, ToolRenderResultOptions{Expanded: expanded}, theme,
		&ToolRenderContext{Cwd: "/tmp/proj", ShowImages: false, IsError: result.IsError})
	return trimRenderedRows(component.Render(width))
}

// trimRenderedRows strips ANSI and the theme's padding to the render width.
func trimRenderedRows(lines []string) []string {
	out := make([]string, len(lines))
	for index, line := range lines {
		out[index] = strings.TrimRight(coding.StripAnsi(line), " ")
	}
	return out
}

// codingWidth is the visible width of a rendered row.
func codingWidth(row string) int {
	return len([]rune(row))
}

// TestMcpResultExpansionKeepsThePreviewInPlace is the other half of D397: the
// preview rows keep their text and order, and expanding only drops the hint row
// above them and appends the rows past the preview.
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
	// The shell's spacer line is unchanged; the hint above the preview is gone
	// and the same preview lines follow.
	if expanded[0] != collapsed[0] {
		t.Fatalf("first row moved: collapsed %q, expanded %q", collapsed[0], expanded[0])
	}
	if !strings.Contains(collapsed[1], "more lines,") {
		t.Fatalf("collapsed row 1 = %q, want the hint above the preview", collapsed[1])
	}
	for index := 0; index < mcpOutputPreviewLines; index++ {
		if expanded[1+index] != collapsed[2+index] {
			t.Errorf("preview row %d: collapsed %q, expanded %q", index, collapsed[2+index], expanded[1+index])
		}
	}
}

// Reference: pi v1.5.2 VisualLinePreview truncates the expand hint with
// truncateToWidth(hint, width, "..."), so a narrow frame keeps the hint on one
// line instead of overflowing.
func TestMcpToolHintTruncatesToTheFrameWidth(t *testing.T) {
	theme := newRendererTestTheme(t)
	lines := make([]string, 40)
	for index := range lines {
		lines[index] = "line " + itoa(index)
	}
	const width = 24
	rendered := renderMcpResultAt(t, textResult(strings.Join(lines, "\n")), false, theme, width)
	hint := rendered[1]
	if got := tui.VisibleWidth(hint); got > width {
		t.Fatalf("hint = %q (%d columns), want at most %d", hint, got, width)
	}
	if !strings.HasSuffix(hint, "...") {
		t.Fatalf("hint = %q, want a trailing ellipsis", hint)
	}
}

// lastNonBlankRow returns the last row that has text after its padding.
func lastNonBlankRow(rows []string) string {
	for index := len(rows) - 1; index >= 0; index-- {
		if row := strings.TrimSpace(rows[index]); row != "" {
			return row
		}
	}
	return ""
}

// TestMcpResultShowsElapsedAndTook pins D493: an MCP call frame carries the
// call's clock at the bottom, a muted `Elapsed <time>` while it runs and
// `Took <time>` once the result is final (the shell and codemode renderers'
// toolTimerText). The reference's MCP renderer shows neither, so a long call
// gave no sign of how long it had been running.
func TestMcpResultShowsElapsedAndTook(t *testing.T) {
	theme := newRendererTestTheme(t)
	now := int64(1_700_000_000_000)
	previous := shellNow
	shellNow = func() time.Time { return time.UnixMilli(now) }
	t.Cleanup(func() { shellNow = previous })

	renderers := mcpRenderers("docs/search")
	context := &ToolRenderContext{Cwd: "/tmp/proj", ExecutionStarted: true}
	renderers.RenderCall(json.RawMessage(`{"path":"/tmp/a"}`), theme, context)

	partial := renderers.RenderResult(textResult("ok"),
		ToolRenderResultOptions{IsPartial: true}, theme, &ToolRenderContext{Cwd: "/tmp/proj",
			ExecutionStarted: true, State: context.State, IsPartial: true})
	last := strings.TrimSpace(lastNonBlankRow(partial.Render(80)))
	if !strings.Contains(last, "Elapsed 0.0s") {
		t.Fatalf("running call's last row = %q, want an elapsed label", last)
	}

	now += 2500
	final := renderers.RenderResult(textResult("ok"), ToolRenderResultOptions{}, theme,
		&ToolRenderContext{Cwd: "/tmp/proj", ExecutionStarted: true, State: context.State})
	last = strings.TrimSpace(lastNonBlankRow(final.Render(80)))
	if !strings.Contains(last, "Took 2.5s") {
		t.Fatalf("final call's last row = %q, want the took label", last)
	}

	// A replayed result carries the tool's recorded time instead of the clock.
	recorded := int64(1200)
	replayed := renderers.RenderResult(textResult("ok"), ToolRenderResultOptions{}, theme,
		&ToolRenderContext{Cwd: "/tmp/proj", DurationMs: &recorded})
	if last := strings.TrimSpace(lastNonBlankRow(replayed.Render(80))); !strings.Contains(last, "Took 1.2s") {
		t.Fatalf("replayed call's last row = %q, want the recorded time", last)
	}
}
