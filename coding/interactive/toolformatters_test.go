package interactive

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

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

// Upstream formatShellCall uses a truthy timeout and interpolates the number
// verbatim, so a zero timeout is omitted and a fractional one keeps its value.
func TestFormatShellCallTimeoutSuffix(t *testing.T) {
	useBuiltinPierThemeForTest(t)
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
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	var lines []string
	for i := range 25 {
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
	useBuiltinPierThemeForTest(t)
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
	useBuiltinPierThemeForTest(t)
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
	useBuiltinPierThemeForTest(t)
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
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	zero := 0.0

	findResult := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: "a"}},
		Details: &coding.FindToolDetails{ResultLimitReached: &zero}}
	if got := formatSearchResult(findResult, ToolRenderResultOptions{}, theme, false, 20); strings.Contains(got, "results limit") {
		t.Fatalf("find zero limit = %q", got)
	}

	lsResult := &SortToolResultContent{Details: &coding.LsToolDetails{EntryLimitReached: &zero}}
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
	useBuiltinPierThemeForTest(t)
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
	useBuiltinPierThemeForTest(t)
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

// pi v1.5.0 render-utils.ts shortenPath uses shortenHomePath: only a path inside
// home is shortened, so a sibling that shares the prefix stays intact.
func TestShortenPathRequiresHomeContainment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	inside := filepath.Join(home, "proj", "a.txt")
	if got := ShortenPath(inside); got != "~"+string(filepath.Separator)+filepath.Join("proj", "a.txt") {
		t.Fatalf("inside = %q", got)
	}
	sibling := filepath.Join(home+"2", "a.txt")
	if got := ShortenPath(sibling); got != sibling {
		t.Fatalf("sibling = %q", got)
	}
}

// The session stores a tool's details as the JSON the tool returned
// (sortToolResultFromAgent), so the read renderer must decode that shape the way
// the other renderers do; pi v1.5.2 (b4afdddf) tools/renderers/read.ts reads
// details.truncation whatever the carrier is.
func TestFormatReadResultReadsTheDetailsTheSessionStores(t *testing.T) {
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	result := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "a\nb"}},
		Details: json.RawMessage(`{"truncation":{"truncated":true,"truncatedBy":"lines","outputLines":5,"totalLines":100,"maxLines":5}}`),
	}
	got := formatReadResult(map[string]any{"path": "x.txt"}, result, ToolRenderResultOptions{Expanded: true}, theme, false, false)
	if !strings.Contains(got, "5 line limit") {
		t.Fatalf("read truncation = %q; want the line-limit notice", got)
	}
}

// pi v1.5.2 (b4afdddf) tools/renderers/read.ts builds the compact row's hint
// with keyText, not keyDisplayText, so the key keeps its configured casing.
func TestCompactReadCallKeepsTheConfiguredKeyCasing(t *testing.T) {
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	theme := ActiveTheme()
	previous := tui.GetKeybindings()
	tui.SetKeybindings(NewAppKeybindingsManager(nil, "").KeybindingsManager)
	defer tui.SetKeybindings(previous)

	got := coding.StripAnsi(formatCompactReadCall(
		compactReadClassification{kind: "resource", label: "AGENTS.md"},
		map[string]any{"path": "AGENTS.md"}, theme))
	want := "(" + KeyText("app.tools.expand") + " to expand)"
	if !strings.Contains(got, want) {
		t.Fatalf("compact read hint = %q, want %q", got, want)
	}
}

func TestEditToolHeaderPrefersFilePathOverPath(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)

	component := NewToolExecutionComponent("edit", "call-1", map[string]any{
		"path":      "/tmp/fallback.txt",
		"file_path": "/tmp/preferred.txt",
		"edits": []any{map[string]any{
			"oldText": "old",
			"newText": "new",
		}},
	}, ToolExecutionOptions{}, &editRenderers, nil, "/tmp")

	got := strings.Join(component.Render(80), "\n")
	if !strings.Contains(got, "preferred.txt") {
		t.Fatalf("edit header %q does not show file_path", got)
	}
	if strings.Contains(got, "fallback.txt") {
		t.Fatalf("edit header %q shows path instead of file_path", got)
	}
}
