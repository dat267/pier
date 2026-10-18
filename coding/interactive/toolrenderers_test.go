package interactive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

func TestToolTruncationWarningsFormatFractionalLimits(t *testing.T) {
	match := 1.5
	result := 2.5
	entries := 3.5
	gotMatch, _, _, _ := toolTruncationWarnings(&coding.GrepToolDetails{MatchLimitReached: &match})
	gotResult, _, _, _ := toolTruncationWarnings(&coding.FindToolDetails{ResultLimitReached: &result})
	gotEntries, _, _, _ := toolTruncationWarnings(&coding.LsToolDetails{EntryLimitReached: &entries})
	if gotMatch != "1.5 matches limit" || gotResult != "2.5 results limit" || gotEntries != "3.5 entries limit" {
		t.Fatalf("fractional warnings = %q, %q, %q", gotMatch, gotResult, gotEntries)
	}
}

func homeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func newRendererTestTheme(t *testing.T) *Theme {
	t.Helper()
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)
	return ActiveTheme()
}

func renderCall(name string, args any, theme *Theme) string {
	renderers := WithBuiltInRenderers(name, nil)
	if renderers == nil {
		return ""
	}
	component := renderers.RenderCall(args, theme, &ToolRenderContext{Cwd: "/tmp/proj"})
	return coding.StripAnsi(strings.Join(component.Render(120), "\n"))
}

func renderResult(name string, result *SortToolResultContent, expanded bool, theme *Theme) string {
	renderers := WithBuiltInRenderers(name, nil)
	if renderers == nil {
		return ""
	}
	component := renderers.RenderResult(result, ToolRenderResultOptions{Expanded: expanded}, theme,
		&ToolRenderContext{Cwd: "/tmp/proj", Args: nil, ShowImages: false})
	return coding.StripAnsi(strings.Join(component.Render(120), "\n"))
}

func textResult(text string) *SortToolResultContent {
	return &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: text}}}
}

// TestToolRenderersCallHeaders covers the upstream call headers (the ones that
// used to render as raw JSON).
func TestToolRenderersCallHeaders(t *testing.T) {
	theme := newRendererTestTheme(t)

	args := func(raw string) any { return json.RawMessage(raw) }

	if got := renderCall("read", args(`{"file_path":"/tmp/proj/main.go"}`), theme); !strings.Contains(got, "read /tmp/proj/main.go") {
		t.Fatalf("read call = %q", got)
	}
	// Build the home case with json.Marshal: concatenating a Windows path into
	// JSON leaves its backslashes unescaped (\U, \n), which is invalid JSON and
	// renders as the empty-path fallback. upstream shortenPath swaps the home
	// prefix for "~" and keeps the remainder verbatim, so the separator here is
	// the platform's.
	homePath := filepath.Join(homeDir(t), "notes.md")
	homeArgs, err := json.Marshal(map[string]string{"file_path": homePath})
	if err != nil {
		t.Fatal(err)
	}
	if got := renderCall("read", args(string(homeArgs)), theme); !strings.Contains(got, "read ~"+strings.TrimPrefix(homePath, homeDir(t))) {
		t.Fatalf("read home call = %q", got)
	}
	if got := renderCall("read", args(`{"file_path":"/tmp/proj/main.go","offset":10,"limit":5}`), theme); !strings.Contains(got, ":10-14") {
		t.Fatalf("read call range = %q", got)
	}
	if got := renderCall("read", args(`{"file_path":"/tmp/proj/main.go","offset":1.5,"limit":1.5}`), theme); !strings.Contains(got, ":1.5-2") {
		t.Fatalf("fractional read call range = %q", got)
	}
	if got := renderCall("read", args(`{"file_path":"/tmp/proj/main.go","limit":0}`), theme); !strings.Contains(got, ":1 ") || strings.Contains(got, ":1-0") {
		t.Fatalf("zero read limit range = %q", got)
	}
	if got := renderCall("bash", args(`{"command":"ls -la"}`), theme); !strings.Contains(got, "$ ls -la") {
		t.Fatalf("bash call = %q", got)
	}
	if got := renderCall("grep", args(`{"pattern":"TODO","path":"/tmp/proj/src"}`), theme); !strings.Contains(got, "grep /TODO/ in /tmp/proj/src") {
		t.Fatalf("grep call = %q", got)
	}
	if got := renderCall("grep", args(`{"pattern":"TODO","limit":1.5}`), theme); !strings.Contains(got, "limit 1.5") {
		t.Fatalf("fractional grep call = %q", got)
	}
	if got := renderCall("find", args(`{"pattern":"*.go"}`), theme); !strings.Contains(got, "find *.go in .") {
		t.Fatalf("find call = %q", got)
	}
	if got := renderCall("find", args(`{"pattern":"*.go","limit":1.5}`), theme); !strings.Contains(got, "limit 1.5") {
		t.Fatalf("fractional find call = %q", got)
	}
	if got := renderCall("ls", args(`{}`), theme); !strings.Contains(got, "ls .") {
		t.Fatalf("ls call = %q", got)
	}
	if got := renderCall("ls", args(`{"limit":1.5}`), theme); !strings.Contains(got, "limit 1.5") {
		t.Fatalf("fractional ls call = %q", got)
	}
	if got := renderCall("write", args(`{"file_path":"/tmp/proj/a.go","content":"package main"}`), theme); !strings.Contains(got, "write /tmp/proj/a.go") {
		t.Fatalf("write call = %q", got)
	}
	if got := renderCall("edit", args(`{"file_path":"/tmp/proj/a.go","oldText":"a","newText":"b"}`), theme); !strings.Contains(got, "edit /tmp/proj/a.go") {
		t.Fatalf("edit call = %q", got)
	}
	// No raw JSON in any header.
	for _, name := range BuiltinToolRendererNames() {
		got := renderCall(name, args(`{"file_path":"/tmp/proj/main.go"}`), theme)
		if strings.Contains(got, "{") && strings.Contains(got, "\"file_path\"") {
			t.Errorf("%s call renders raw JSON: %q", name, got)
		}
	}
}

// TestToolRenderersResults covers the collapsed/expanded result bodies.
func TestToolRenderersResults(t *testing.T) {
	theme := newRendererTestTheme(t)

	// Collapsed (not expanded) results are empty for read.
	if got := renderResult("read", textResult("hello"), false, theme); strings.Contains(got, "hello") {
		t.Fatalf("collapsed read result should be empty: %q", got)
	}
	expanded := renderResult("read", textResult("hello"), true, theme)
	if !strings.Contains(expanded, "hello") {
		t.Fatalf("expanded read result = %q", expanded)
	}

	// ls collapses to 20 lines with a truncation hint.
	many := make([]string, 30)
	for index := range many {
		many[index] = "entry"
	}
	lsResult := renderResult("ls", textResult(strings.Join(many, "\n")), false, theme)
	if !strings.Contains(lsResult, "... (10 more lines,") {
		t.Fatalf("ls truncation hint missing: %q", lsResult)
	}

	// Truncation details surface a warning.
	details := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "out"}},
		Details: &coding.LsToolDetails{Truncation: &coding.TruncationResult{
			Truncated: true, TruncatedBy: "bytes", OutputLines: 20, MaxBytes: 50 * 1024,
		}},
	}
	if got := renderResult("ls", details, false, theme); !strings.Contains(got, "[Truncated:") {
		t.Fatalf("ls truncation warning missing: %q", got)
	}
}

// TestEditRendererPreview covers the edit preview diff.
func TestEditRendererPreviewReplacesMalformedUTF8(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "invalid.txt")
	if err := os.WriteFile(file, []byte{0xff, '\n', 't', 'a', 'r', 'g', 'e', 't', '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	preview := computeEditsPreview(file, []coding.Edit{{OldText: "target", NewText: "changed"}}, dir)
	if preview == nil || preview.Error != "" || !strings.Contains(preview.Diff, "\uFFFD") {
		t.Fatalf("edit preview = %+v; want replacement-decoded context", preview)
	}
}

func TestEditRendererPreview(t *testing.T) {
	newRendererTestTheme(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := computeEditsPreview(file, []coding.Edit{{OldText: "two", NewText: "TWO"}}, dir)
	if preview == nil || preview.Error != "" {
		t.Fatalf("preview = %+v", preview)
	}
	if !strings.Contains(preview.Diff, "TWO") || !strings.Contains(preview.Diff, "one") {
		t.Fatalf("preview diff = %q", preview.Diff)
	}
	if preview.FirstChangedLine == nil || *preview.FirstChangedLine != 2 {
		t.Fatalf("firstChangedLine = %+v", preview.FirstChangedLine)
	}

	// A missing file reports the upstream error shape.
	missing := computeEditsPreview(filepath.Join(dir, "gone.txt"), []coding.Edit{{OldText: "a", NewText: "b"}}, dir)
	if missing == nil || !strings.Contains(missing.Error, "Could not edit file") {
		t.Fatalf("missing preview = %+v", missing)
	}
}

// TestWithBuiltInRenderers covers the merge semantics.
func TestWithBuiltInRenderers(t *testing.T) {
	// Unknown tools pass the definition through (nil stays nil).
	if got := WithBuiltInRenderers("unknown-tool", nil); got != nil {
		t.Fatalf("unknown tool = %+v", got)
	}
	custom := &ToolRenderers{RenderShell: "self"}
	if got := WithBuiltInRenderers("unknown-tool", custom); got != custom {
		t.Fatalf("unknown tool with definition = %+v", got)
	}
	// Built-in merge keeps the custom call renderer and fills the result.
	merged := WithBuiltInRenderers("read", &ToolRenderers{RenderShell: "self"})
	if merged == nil || merged.RenderShell != "self" || merged.RenderCall == nil || merged.RenderResult == nil {
		t.Fatalf("merged = %+v", merged)
	}
}

// TestToolRendererShellComponent verifies the collapsed bash preview component
// renders the tail of the output.
func TestToolRendererShellComponent(t *testing.T) {
	theme := newRendererTestTheme(t)
	lines := make([]string, 40)
	for index := range lines {
		lines[index] = "line"
	}
	result := textResult(strings.Join(lines, "\n"))
	component := bashRenderers.RenderResult(result, ToolRenderResultOptions{Expanded: false}, theme,
		&ToolRenderContext{Cwd: "/tmp/proj"})
	rendered := coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "earlier lines") {
		t.Fatalf("bash preview = %q", rendered)
	}
	if !strings.Contains(rendered, "line") {
		t.Fatalf("bash preview missing output: %q", rendered)
	}
	_ = tui.NewSpacer(1)
}

// TestShellElapsedDuration covers the running-time display for shell tools: a
// partial result keeps the timer running and the label keeps ticking (upstream
// arms a 1s invalidate), and the final result freezes it as "Took". The port
// set endedAtMS on the *partial* update (inverting upstream's
// `!isPartial || isError`), so the timer froze at the first update, and it
// never armed a redraw, so the elapsed value never advanced.
func TestShellElapsedDuration(t *testing.T) {
	theme := newRendererTestTheme(t)
	ctx := &ToolRenderContext{Cwd: "/tmp/proj", Args: map[string]any{"command": "sleep 5"}, ExecutionStarted: true}
	bashRenderers.RenderCall(ctx.Args, theme, ctx)
	state, ok := ctx.State.(*shellCallState)
	if !ok || state.startedAtMS == 0 {
		t.Fatalf("execution start did not start the timer: %+v", ctx.State)
	}

	result := textResult("partial output")
	component := bashRenderers.RenderResult(result, ToolRenderResultOptions{IsPartial: true}, theme, ctx)
	if state.endedAtMS != 0 {
		t.Fatal("a partial result must not stop the timer")
	}

	// The running label recomputes from the clock on every render.
	state.startedAtMS = time.Now().Add(-5 * time.Second).UnixMilli()
	rendered := coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Elapsed 5.") {
		t.Fatalf("running duration = %q, want an Elapsed 5.x", rendered)
	}

	// The component animates while the call runs so the label keeps ticking.
	container, ok := component.(*tui.Container)
	if !ok {
		t.Fatalf("result component = %T", component)
	}
	animated := false
	for _, child := range container.Children {
		if animator, ok := child.(tui.Animator); ok {
			want, delay := animator.AnimationFrame(time.Now())
			// D238: the port ticks the elapsed label at 100ms so its tenths
			// display resolution moves by .1s (upstream redraws at 1000ms).
			if !want || delay != shellElapsedTick {
				t.Fatalf("running animator = want %v delay %v, want a %v tick", want, delay, shellElapsedTick)
			}
			animated = true
		}
	}
	if !animated {
		t.Fatal("running tool has no animator driving the elapsed label")
	}

	// The final result freezes the label as "Took".
	component = bashRenderers.RenderResult(result, ToolRenderResultOptions{}, theme, ctx)
	if state.endedAtMS == 0 {
		t.Fatal("the final result must stop the timer")
	}
	rendered = coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Took ") {
		t.Fatalf("final duration = %q, want a Took line", rendered)
	}
	for _, child := range component.(*tui.Container).Children {
		if animator, ok := child.(tui.Animator); ok {
			if want, _ := animator.AnimationFrame(time.Now()); want {
				t.Fatal("a finished tool must not keep animating")
			}
		}
	}
}

// TestFormatDurationTenths pins the 0.1s display resolution of the shell
// elapsed label (upstream render-utils.ts displays tenths but redraws every second).
func TestFormatDurationTenths(t *testing.T) {
	for _, tc := range []struct {
		ms   float64
		want string
	}{
		{ms: 3450, want: "3.5s"},
		{ms: 10, want: "0.0s"},
		// JS toFixed(1) rounds half away from zero on the double value.
		{ms: 1250, want: "1.3s"},
		{ms: 1150, want: "1.1s"},
		{ms: 60_000, want: "1m 0s"},
		{ms: 3_600_000, want: "1h 0m 0s"},
	} {
		if got := formatDuration(tc.ms); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

// #10549: the label shows the tool's recorded execution time, so a replayed result (which has
// no start or end event and therefore no timestamps) still reports Took, and a live call does
// not count the wall-clock steps between the events.
func TestShellElapsedUsesTheRecordedDuration(t *testing.T) {
	theme := newRendererTestTheme(t)
	recorded := int64(1500)

	// Replay: the session re-renders the tool result with its recorded duration and nothing else.
	ctx := &ToolRenderContext{Cwd: "/tmp/proj", Args: map[string]any{"command": "sleep 1.5"}, DurationMs: &recorded}
	component := bashRenderers.RenderResult(textResult("done"), ToolRenderResultOptions{}, theme, ctx)
	rendered := coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Took 1.5s") {
		t.Fatalf("replayed label = %q, want Took 1.5s", rendered)
	}
	if strings.Contains(rendered, "Elapsed") {
		t.Fatalf("a replayed result must not show a running label: %q", rendered)
	}

	// Live: the wall clock says five seconds, the tool reported 1.5.
	ctx = &ToolRenderContext{Cwd: "/tmp/proj", Args: map[string]any{"command": "sleep 1.5"}, ExecutionStarted: true}
	bashRenderers.RenderCall(ctx.Args, theme, ctx)
	state, ok := ctx.State.(*shellCallState)
	if !ok {
		t.Fatalf("state = %T", ctx.State)
	}
	state.startedAtMS = time.Now().Add(-5 * time.Second).UnixMilli()
	ctx.DurationMs = &recorded
	component = bashRenderers.RenderResult(textResult("done"), ToolRenderResultOptions{}, theme, ctx)
	rendered = coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Took 1.5s") || strings.Contains(rendered, "Took 5.") {
		t.Fatalf("live label = %q, want the recorded 1.5s rather than five seconds of wall clock", rendered)
	}
}

// Upstream formatShellCall uses a truthy timeout and interpolates the number
// verbatim, so a zero timeout is omitted and a fractional one keeps its value.
func TestFormatShellCallTimeoutSuffix(t *testing.T) {
	installPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	if got := formatShellCall(map[string]any{"command": "ls", "timeout": 0.0}, "bash", theme); strings.Contains(got, "timeout") {
		t.Fatalf("zero timeout = %q; want no suffix", got)
	}
	got := formatShellCall(map[string]any{"command": "ls", "timeout": 2.5}, "bash", theme)
	if !strings.Contains(got, "(timeout 2.5s)") {
		t.Fatalf("fractional timeout = %q; want 2.5s", got)
	}
}

// Upstream search/ls result renderers show every line when expanded
// (`maxLines = options.expanded ? lines.length : N`).
func TestFormatSearchResultExpandedShowsAllLines(t *testing.T) {
	installPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	var lines []string
	for i := 0; i < 25; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Join(lines, "\n")}}}
	got := formatSearchResult(result, ToolRenderResultOptions{Expanded: true}, theme, false, 15)
	if strings.Contains(got, "more lines") {
		t.Fatalf("expanded result still truncated: %q", got)
	}
}

// A known language in a read preview is syntax highlighted (D231),
// with the source preserved after the ANSI is stripped.
func TestFormatReadResultHighlightsKnownLanguage(t *testing.T) {
	installPierThemeForTest(t)
	SetTrueColorSupport(true)
	InitTheme("dark", false)
	theme := ActiveTheme()
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: "func answer() int { return 42 }"}}}

	got := formatReadResult(map[string]any{"path": "answer.go"}, result,
		ToolRenderResultOptions{Expanded: true}, theme, false, false)
	if !strings.Contains(coding.StripAnsi(got), "func answer() int { return 42 }") {
		t.Fatalf("read result lost source text: %q", got)
	}
	if want := theme.Fg("syntaxKeyword", "func"); !strings.Contains(got, want) {
		t.Fatalf("read result missing keyword color: %q", got)
	}
	if want := theme.Fg("syntaxNumber", "42"); !strings.Contains(got, want) {
		t.Fatalf("read result missing number color: %q", got)
	}
}

// Upstream uses truncation.maxLines (falling back to DEFAULT_MAX_LINES) in the
// read line-limit notice.
func TestFormatReadResultUsesTruncationMaxLines(t *testing.T) {
	installPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	result := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "a\nb\nc"}},
		Details: &coding.ReadToolDetails{Truncation: &coding.TruncationResult{
			Truncated: true, TruncatedBy: "lines", OutputLines: 5, TotalLines: 100, MaxLines: 5,
		}},
	}
	got := formatReadResult(map[string]any{"path": "x.txt"}, result, ToolRenderResultOptions{Expanded: true}, theme, false, false)
	if !strings.Contains(got, "(5 line limit)") {
		t.Fatalf("read truncation = %q; want 5 line limit", got)
	}
}

// Upstream uses truncation.maxBytes (falling back to DEFAULT_MAX_BYTES) in the
// read byte-limit notices.
func TestFormatReadResultUsesTruncationMaxBytes(t *testing.T) {
	installPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	options := ToolRenderResultOptions{Expanded: true}

	byteLimited := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "a\nb\nc"}},
		Details: &coding.ReadToolDetails{Truncation: &coding.TruncationResult{
			Truncated: true, TruncatedBy: "bytes", OutputLines: 3, TotalLines: 100, MaxBytes: 1234,
		}},
	}
	if got := formatReadResult(map[string]any{"path": "x.txt"}, byteLimited, options, theme, false, false); !strings.Contains(got, coding.FormatSize(1234)+" limit") {
		t.Fatalf("byte truncation = %q; want %s limit", got, coding.FormatSize(1234))
	}

	firstLine := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "a"}},
		Details: &coding.ReadToolDetails{Truncation: &coding.TruncationResult{
			Truncated: true, FirstLineExceedsLimit: true, MaxBytes: 1234,
		}},
	}
	if got := formatReadResult(map[string]any{"path": "x.txt"}, firstLine, options, theme, false, false); !strings.Contains(got, "First line exceeds "+coding.FormatSize(1234)) {
		t.Fatalf("first-line truncation = %q; want %s", got, coding.FormatSize(1234))
	}
}

// Upstream guards the limit warnings with truthiness (`if (resultLimit)`), so a
// zero limit is not reported.
func TestTruncationWarningsSkipZeroLimits(t *testing.T) {
	installPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	zero := 0.0

	findResult := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: "a"}},
		Details: &coding.FindToolDetails{ResultLimitReached: &zero}}
	if got := formatSearchResult(findResult, ToolRenderResultOptions{}, theme, false, 20); strings.Contains(got, "results limit") {
		t.Fatalf("find zero limit = %q", got)
	}

	lsResult := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: "a"}},
		Details: &coding.LsToolDetails{EntryLimitReached: &zero}}
	if got := truncationSuffix(lsResult.Details, theme); strings.Contains(got, "entries limit") {
		t.Fatalf("ls zero limit = %q", got)
	}

	grepResult := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: "a"}},
		Details: &coding.GrepToolDetails{MatchLimitReached: &zero}}
	if got := formatSearchResult(grepResult, ToolRenderResultOptions{}, theme, false, 15); strings.Contains(got, "matches limit") {
		t.Fatalf("grep zero limit = %q", got)
	}
}

// Upstream's edit error text maps every text block (`c.text || ""`) and joins
// them, so an empty text block still contributes its separator newline.
func TestFormatEditResultKeepsEmptyTextBlocks(t *testing.T) {
	installPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	result := &SortToolResultContent{Content: []ToolResultContent{
		{Type: "text", Text: ""},
		{Type: "text", Text: "boom"},
	}, IsError: true}
	got := coding.StripAnsi(formatEditResult(&editRenderArgs{Path: "x.txt"}, nil, result, theme, true))
	if got != "\nboom" {
		t.Fatalf("edit error = %q; want %q", got, "\nboom")
	}
}

// Upstream's edit preview reports the Node error code, not the Go error text.
func TestComputeEditsPreviewReportsNodeErrorCode(t *testing.T) {
	dir := t.TempDir()
	preview := computeEditsPreview(filepath.Join(dir, "gone.txt"), []coding.Edit{{OldText: "a", NewText: "b"}}, dir)
	if preview == nil || !strings.Contains(preview.Error, "Error code: ENOENT.") {
		t.Fatalf("missing preview = %+v", preview)
	}
}

// Upstream checks access first (wrapping its failure) and lets the read error
// propagate raw, so a directory reports the read error rather than the wrapper.
func TestComputeEditsPreviewDirectoryReportsRawError(t *testing.T) {
	dir := t.TempDir()
	preview := computeEditsPreview(dir, []coding.Edit{{OldText: "a", NewText: "b"}}, filepath.Dir(dir))
	if preview == nil || preview.Error == "" {
		t.Fatalf("directory preview = %+v", preview)
	}
	if strings.Contains(preview.Error, "Could not edit file") {
		t.Fatalf("directory preview wrapped the read error: %q", preview.Error)
	}
}

// Upstream's write error text maps every text block (`c.text || ""`) and joins
// them, so an empty text block still contributes its separator newline.
func TestFormatWriteResultKeepsEmptyTextBlocks(t *testing.T) {
	installPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	result := &SortToolResultContent{Content: []ToolResultContent{
		{Type: "text", Text: ""},
		{Type: "text", Text: "boom"},
	}}
	got := coding.StripAnsi(formatWriteResult(result, true, theme))
	if got != "\n\nboom" {
		t.Fatalf("write error = %q; want \\n\\nboom", got)
	}
}

// Upstream's recorded-duration branch requires !isPartial, so a partial result
// that carries a duration still shows "Elapsed".
func TestShellElapsedPartialWithDurationStaysElapsed(t *testing.T) {
	theme := newRendererTestTheme(t)
	ctx := &ToolRenderContext{Cwd: "/tmp/proj", Args: map[string]any{"command": "sleep 5"}, ExecutionStarted: true}
	bashRenderers.RenderCall(ctx.Args, theme, ctx)
	duration := int64(5000)
	ctx.DurationMs = &duration
	component := bashRenderers.RenderResult(textResult("partial output"), ToolRenderResultOptions{IsPartial: true}, theme, ctx)
	rendered := coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Elapsed ") || strings.Contains(rendered, "Took ") {
		t.Fatalf("partial with duration = %q; want Elapsed", rendered)
	}
}
