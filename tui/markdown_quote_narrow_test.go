package tui

import (
	"strings"
	"testing"
)

// A blockquote border is two columns, so a narrow view cannot hold one per
// nesting level. pi v1.5.2 (b4afdddf) components/markdown.ts renders the nested
// content without a border when `width < 3`, because each level that re-wraps
// its children at width 1 grows the output exponentially with nesting depth.
func TestNestedBlockquotesStopAtTheBorderThatFits(t *testing.T) {
	identity := func(text string) string { return text }
	theme := MarkdownTheme{
		Heading: identity, Link: identity, LinkURL: identity, Code: identity,
		CodeBlock: identity, CodeBlockBorder: identity, Quote: identity, QuoteBorder: identity,
		Hr: identity, ListBullet: identity, Bold: identity, Italic: identity,
		Strikethrough: identity, Underline: identity,
	}
	cases := []struct {
		depth, width int
		want         []string
	}{
		{1, 1, []string{"x"}},
		{4, 1, []string{"x"}},
		{1, 2, []string{"x "}},
		{4, 2, []string{"x "}},
		{1, 3, []string{"│ x"}},
		{2, 3, []string{"│ x"}},
		{10, 8, []string{"│ │ │ x "}},
		{12, 20, []string{"│ │ │ │ │ │ │ │ │ x "}},
		{12, 40, []string{"│ │ │ │ │ │ │ │ │ │ │ │ x               "}},
	}
	for _, tc := range cases {
		source := strings.Repeat("> ", tc.depth) + "x"
		got := NewMarkdown(source, 0, 0, theme, nil, MarkdownOptions{}).Render(tc.width)
		if len(got) != len(tc.want) {
			t.Fatalf("depth %d width %d: %d lines, want %d\ngot %q", tc.depth, tc.width, len(got), len(tc.want), got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("depth %d width %d line %d: %q, want %q", tc.depth, tc.width, i, got[i], tc.want[i])
			}
		}
	}
}
