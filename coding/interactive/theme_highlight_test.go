package interactive

import (
	"slices"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/highlight"
	"github.com/dat267/pier/tui"
)

func highlightThemeFixture(t *testing.T) *Theme {
	t.Helper()
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	InitTheme("dark", false)
	return ActiveTheme()
}

func scopeNode(kind string, children ...*highlight.Node) *highlight.Node {
	return &highlight.Node{Kind: kind, Children: children}
}

func textNode(text string) *highlight.Node { return &highlight.Node{Text: text} }

func TestHighlightCodeFallsBackForUnknownLanguage(t *testing.T) {
	theme := highlightThemeFixture(t)

	got := HighlightCode("plain text", "not-a-real-language")
	want := []string{theme.Fg("mdCodeBlock", "plain text")}
	if !slices.Equal(got, want) {
		t.Fatalf("unknown-language highlight = %q, want %q", got, want)
	}
}

// Every line of a multi-line fence is styled and returned in order.
func TestHighlightCodeStylesEveryLine(t *testing.T) {
	highlightThemeFixture(t)

	got := HighlightCode("a\nb", "go")
	if len(got) != 2 {
		t.Fatalf("lines = %d, want 2", len(got))
	}
	for i, line := range got {
		if coding.StripAnsi(line) != []string{"a", "b"}[i] {
			t.Fatalf("line %d = %q", i, coding.StripAnsi(line))
		}
	}
}

// The renderer maps a scope to its formatter and leaves surrounding text alone.
func TestRenderHighlightedNodesAppliesScopeFormatters(t *testing.T) {
	theme := highlightThemeFixture(t)

	got := renderHighlightedNodes([]*highlight.Node{
		scopeNode("keyword", textNode("const")),
		textNode(" value"),
	}, theme)
	want := theme.Fg("syntaxKeyword", "const") + " value"
	if got != want {
		t.Fatalf("rendered = %q, want %q", got, want)
	}
}

// A nested scope with no formatter keeps the nearest outer scope's formatting.
func TestRenderHighlightedNodesInheritsParentForUnmappedScope(t *testing.T) {
	theme := highlightThemeFixture(t)

	got := renderHighlightedNodes([]*highlight.Node{
		scopeNode("string",
			textNode("a"),
			scopeNode("section", textNode("b")),
			textNode("c")),
	}, theme)
	want := theme.Fg("syntaxString", "a") + theme.Fg("syntaxString", "b") + theme.Fg("syntaxString", "c")
	if got != want {
		t.Fatalf("rendered = %q, want %q", got, want)
	}
}

// A sublanguage node's class is a language name, not an `hljs-` scope, so it
// does not interrupt the outer scope.
func TestRenderHighlightedNodesKeepsParentAcrossSublanguage(t *testing.T) {
	theme := highlightThemeFixture(t)

	got := renderHighlightedNodes([]*highlight.Node{
		scopeNode("string",
			textNode("a"),
			&highlight.Node{Kind: "xml", Sublanguage: true, Children: []*highlight.Node{textNode("b")}},
			textNode("c")),
	}, theme)
	want := theme.Fg("syntaxString", "a") + theme.Fg("syntaxString", "b") + theme.Fg("syntaxString", "c")
	if got != want {
		t.Fatalf("rendered = %q, want %q", got, want)
	}
}

// The reference resolves `title.function_` to the `title` formatter.
func TestRenderHighlightedNodesResolvesDottedScopePrefix(t *testing.T) {
	theme := highlightThemeFixture(t)

	got := renderHighlightedNodes([]*highlight.Node{scopeNode("title.function_", textNode("f"))}, theme)
	want := theme.Fg("syntaxFunction", "f")
	if got != want {
		t.Fatalf("rendered = %q, want %q", got, want)
	}
}

// The reference resolves `meta-string` to the `meta` formatter.
func TestRenderHighlightedNodesResolvesDashedScopePrefix(t *testing.T) {
	theme := highlightThemeFixture(t)

	got := renderHighlightedNodes([]*highlight.Node{scopeNode("meta-string", textNode("x"))}, theme)
	want := theme.Fg("muted", "x")
	if got != want {
		t.Fatalf("rendered = %q, want %q", got, want)
	}
}

// Each line of a multi-line scope is colored independently, and an empty line
// stays empty (upstream #10143).
func TestHighlightCodeColorsPythonDocstringLinesIndependently(t *testing.T) {
	theme := highlightThemeFixture(t)

	code := "\"\"\"\nline one\n\nline two\n\"\"\"\nafter"
	ansi := theme.GetFgAnsi("syntaxString")
	want := []string{
		ansi + "\"\"\"\x1b[39m",
		ansi + "line one\x1b[39m",
		"",
		ansi + "line two\x1b[39m",
		ansi + "\"\"\"\x1b[39m",
		"after",
	}
	got := HighlightCode(code, "python")
	if !slices.Equal(got, want) {
		t.Fatalf("python docstring = %q, want %q", got, want)
	}
}

// The diff language colors additions and deletions with the tool-diff tokens.
func TestHighlightCodeColorsDiffLines(t *testing.T) {
	theme := highlightThemeFixture(t)

	lines := HighlightCode("-old\n+new\n", "diff")
	if len(lines) < 2 {
		t.Fatalf("lines = %d, want at least 2", len(lines))
	}
	if want := theme.Fg("toolDiffRemoved", "-old"); lines[0] != want {
		t.Fatalf("removed line = %q, want %q", lines[0], want)
	}
	if want := theme.Fg("toolDiffAdded", "+new"); lines[1] != want {
		t.Fatalf("added line = %q, want %q", lines[1], want)
	}
}

// The cli-highlight defaults used by the theme map to their syntax tokens.
func TestHighlightCodeMapsDefaultScopes(t *testing.T) {
	theme := highlightThemeFixture(t)

	if got, want := HighlightCode("@decorator", "python")[0], theme.Fg("muted", "@decorator"); got != want {
		t.Fatalf("@decorator = %q, want %q", got, want)
	}
	js := HighlightCode("const re = /foo+/gi;", "javascript")[0]
	if want := theme.Fg("syntaxString", "/foo+/gi"); !strings.Contains(js, want) {
		t.Fatalf("javascript regexp = %q, want it to contain %q", js, want)
	}
	htmlLine := HighlightCode("<div></div>", "html")[0]
	if want := theme.Fg("syntaxKeyword", "div"); !strings.Contains(htmlLine, want) {
		t.Fatalf("html tag = %q, want it to contain %q", htmlLine, want)
	}
}

// Colored output preserves the source characters; the port walks the scope
// tree directly, so no HTML entity round-trip can alter them.
func TestHighlightCodePreservesSourceCharacters(t *testing.T) {
	highlightThemeFixture(t)

	code := `const s = "<a>&b;"`
	got := strings.Join(HighlightCode(code, "javascript"), "")
	if plain := coding.StripAnsi(got); plain != code {
		t.Fatalf("stripped = %q, want %q", plain, code)
	}
}

// A fenced code block rendered through the markdown theme reaches the
// highlighter and colors its tokens end to end.
func TestMarkdownCodeFenceHighlightsThroughTheTheme(t *testing.T) {
	theme := highlightThemeFixture(t)

	markdown := tui.NewMarkdown("```go\nfunc answer() int { return 42 }\n```", 0, 0, GetMarkdownTheme(), nil, tui.MarkdownOptions{})
	got := strings.Join(markdown.Render(80), "\n")
	if want := theme.Fg("syntaxKeyword", "func"); !strings.Contains(got, want) {
		t.Fatalf("rendered fenced block missing keyword color: %q", got)
	}
}
