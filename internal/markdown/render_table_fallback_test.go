package markdown

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) tui/src/components/markdown.ts:1005-1011. A
// table with no room for its columns falls back to `wrapTextWithAnsi(token.raw,
// availableWidth)`, and marked's table token raw stops at the last row: it has
// no trailing newline. The port's block raw keeps the source's final newline, so
// wrapping it whole produced a trailing empty line, and the space token that
// follows added the real blank line, leaving two.
func TestRendererNarrowTableFallbackAddsOneBlankLine(t *testing.T) {
	got := renderBlocksTest(t, "| a | 漢字 | a |\n| :- | :- | :- |\n\nafter\n", 12, Options{})
	want := []string{
		"| a | 漢字 |",
		"a |",
		"| :- | :- |",
		":- |",
		"",
		"after",
	}
	if len(got) != len(want) {
		t.Fatalf("rendered %d lines, want %d: %q", len(got), len(want), got)
	}
	for index := range want {
		if strings.TrimRight(got[index], " ") != want[index] {
			t.Fatalf("line %d = %q, want %q (all: %q)", index, got[index], want[index], got)
		}
	}
}

// The control: a table with room for its columns still renders as a table.
func TestRendererWideTableStillRendersAsTable(t *testing.T) {
	got := renderBlocksTest(t, "| a | b |\n| :- | :- |\n| 1 | 2 |\n", 40, Options{})
	if len(got) == 0 || !strings.HasPrefix(got[0], "┌") {
		t.Fatalf("wide table = %q, want box drawing", got)
	}
}
