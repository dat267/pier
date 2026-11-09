package tui

import (
	"strings"
	"testing"
)

func mdTrimmedLines(markdown *Markdown, width int) []string {
	lines := markdown.Render(width)
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

// An ordered list keeps the number its first marker carries, zero included:
// pi v1.5.0 (f9a26365) `packages/tui/src/components/markdown.ts` renderList
// reads `typeof token.start === "number" ? token.start : 1`, and marked's list
// tokenizer sets `start: +bull.slice(0, -1)` from the first marker, so `0. a`
// renders `0. a`. Pier normalized a zero start to one and rendered `1. a`.
func TestMarkdownOrderedListKeepsZeroStart(t *testing.T) {
	theme := mdTestTheme()
	got := mdTrimmedLines(NewMarkdown("0. zero\n1. one\n2. two", 0, 0, theme, nil, MarkdownOptions{}), 20)
	want := []string{"<lb>0. </lb>zero", "<lb>1. </lb>one", "<lb>2. </lb>two"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %q, want %q", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// A list that does not start at zero is unchanged by that fix.
func TestMarkdownOrderedListNonZeroStart(t *testing.T) {
	theme := mdTestTheme()
	got := mdTrimmedLines(NewMarkdown("5. five\n6. six", 0, 0, theme, nil, MarkdownOptions{}), 20)
	want := []string{"<lb>5. </lb>five", "<lb>6. </lb>six"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
