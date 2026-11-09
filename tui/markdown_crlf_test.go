package tui

import (
	"strings"
	"testing"
)

// Carriage returns are line terminators, not text: pi v1.5.0 (f9a26365)
// `packages/tui/src/components/markdown.ts` lexes the source with marked, whose
// `Lexer.lex` begins with `src.replace(/\r\n|\r/g, "\n")` (marked 18.0.11
// `carriageReturn`), before any block or inline rule runs. Pier rendered the
// raw CR instead, which put the character inside heading and list text and
// stopped a CRLF table from being recognized.
func TestMarkdownNormalizesCarriageReturns(t *testing.T) {
	theme := mdTestTheme()
	cases := []string{
		"# h\r\nx",
		"# h\rx",
		"- a\r\n- b",
		"1. a\r\n2. b",
		"> q\r\n> r",
		"| a |\r\n| - |\r\n| 1 |",
		"para\r\n\r\nafter",
		"```js\r\nx\r\n```",
		"a  \r\nb",
	}
	for _, src := range cases {
		want := NewMarkdown(strings.ReplaceAll(src, "\r\n", "\n"), 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		lfOnly := NewMarkdown(strings.ReplaceAll(src, "\r", "\n"), 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		_ = lfOnly
		got := NewMarkdown(src, 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		if len(got) != len(want) {
			t.Errorf("%q: got %d lines %q, want %d lines %q", src, len(got), got, len(want), want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%q: line %d got %q, want %q", src, i, got[i], want[i])
				break
			}
		}
	}
}
