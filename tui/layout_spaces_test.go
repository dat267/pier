package tui

import (
	"strings"
	"testing"
)

// Upstream pads with " ".repeat(width), which cannot fail at any width, so the
// static table's finite size must not turn a wide render into a panic. A
// Markdown render pads its lines with repeatSpaces(width), which reaches this
// from the public Render API on a wide terminal or library caller.
func TestRepeatSpacesBeyondTheStaticTable(t *testing.T) {
	width := len(spaces) + 904
	if got := repeatSpaces(width); len(got) != width || got != strings.Repeat(" ", width) {
		t.Fatalf("repeatSpaces(%d) length = %d", width, len(got))
	}

	markdown := NewMarkdown("hello", 1, 1, mdTestTheme(), nil, MarkdownOptions{})
	for _, line := range markdown.Render(width) {
		if visible := VisibleWidth(line); visible > width {
			t.Fatalf("rendered line width = %d, want <= %d", visible, width)
		}
	}
}
