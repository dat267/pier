package tui

import "testing"

// A GFM table followed by a whitespace-only final line must not gain a blank
// line. pi v1.5.0 (f9a26365) `packages/tui/src/components/markdown.ts` lexes
// with marked, whose `table` rule takes a trailing whitespace-only line that
// has no newline after it, as long as it does not begin an indented code line
// (`(?: {4}| {0,3}\t)[^\n]`); `tableRowBlankLine` then drops it from the rows.
// The line renders as nothing, and pier rendered it as an empty line (A439).
func TestMarkdownTableSwallowsTrailingWhitespaceLine(t *testing.T) {
	theme := mdTestTheme()
	table := "| a |\n| - |\n| 1 |"
	base := NewMarkdown(table, 0, 0, theme, nil, MarkdownOptions{}).Render(24)
	for _, suffix := range []string{"\n", "\n ", "\n  ", "\n   ", "\n    "} {
		got := NewMarkdown(table+suffix, 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		if len(got) != len(base) {
			t.Errorf("suffix %q: got %d lines %q, want %d", suffix, len(got), got, len(base))
		}
	}
	// A line that does begin an indented code line still ends the table, so it
	// keeps its own blank line.
	for _, suffix := range []string{"\n     ", "\n \t ", "\n  \n", "\n\n"} {
		got := NewMarkdown(table+suffix, 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		if len(got) != len(base)+1 {
			t.Errorf("suffix %q: got %d lines %q, want %d", suffix, len(got), got, len(base)+1)
		}
	}
}
