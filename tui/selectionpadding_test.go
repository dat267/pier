package tui

import (
	"strings"
	"testing"
)

// selectionHighlightStart is the column where a row's reverse-video run begins.
func selectionHighlightStart(t *testing.T, line string) int {
	t.Helper()
	start := strings.Index(line, "\x1b[7m")
	if start < 0 {
		t.Fatalf("no selection highlight in %q", line)
	}
	return VisibleWidth(StripTerminalSequences(line[:start]))
}

// selectionHighlightWidth measures the reverse-video run painted on a screen
// row.
func selectionHighlightWidth(t *testing.T, line string) int {
	t.Helper()
	start := strings.Index(line, "\x1b[7m")
	if start < 0 {
		t.Fatalf("no selection highlight in %q", line)
	}
	rest := line[start+len("\x1b[7m"):]
	end := strings.Index(rest, "\x1b[27m")
	if end < 0 {
		t.Fatalf("unterminated highlight in %q", line)
	}
	return VisibleWidth(StripTerminalSequences(rest[:end]))
}

// selectionFrame drags from the first cell of the first row to past the end of
// the last and returns the painted rows.
func selectionFrame(t *testing.T, width int, lines []string) []string {
	t.Helper()
	terminal := &recordingTerminal{width: width, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(&scriptedComponent{lines: lines})
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	screen.HandleTerminalInput("\x1b[<0;1;1M")
	screen.HandleTerminalInput("\x1b[<32;" + itoa(width) + ";" + itoa(len(lines)) + "M")
	screen.HandleTerminalInput("\x1b[<0;" + itoa(width) + ";" + itoa(len(lines)) + "m")
	return screen.applySelection(screen.previousScreen, screen.currentLayout)
}

// The right side stops at the row's text: the renderer pads a code block's rows
// to the width, and painting that padding draws a block of spaces past the text.
func TestSelectionHighlightSkipsHorizontalPadding(t *testing.T) {
	out := selectionFrame(t, 20, []string{"hello world", "b", "gamma"})
	if got := selectionHighlightWidth(t, out[0]); got != len("hello world") {
		t.Fatalf("first row highlight = %d cells, want %d", got, len("hello world"))
	}
	if got := selectionHighlightWidth(t, out[1]); got != len("b") {
		t.Fatalf("middle row highlight = %d cells, want %d", got, len("b"))
	}
}

// A selection starts at the margin its rows share, so the transcript's own
// indentation is not painted, including on the row where the drag started.
func TestSelectionHighlightSkipsLeadingPadding(t *testing.T) {
	out := selectionFrame(t, 20, []string{"  alpha", "  beta"})
	for row := range []string{"alpha", "beta"} {
		if got := selectionHighlightStart(t, out[row]); got != 2 {
			t.Fatalf("row %d highlight starts at %d, want the margin at 2", row, got)
		}
	}
}

// Indentation beyond the shared margin is content: a test failure's hanging
// indent stays inside the selection.
func TestSelectionHighlightKeepsLegitimateIndent(t *testing.T) {
	out := selectionFrame(t, 20, []string{"FAIL: x", "     y"})
	if got := selectionHighlightStart(t, out[1]); got != 0 {
		t.Fatalf("indented row highlight starts at %d, want 0", got)
	}
	if !strings.Contains(StripTerminalSequences(out[1]), "     y") {
		t.Fatalf("the indent left the frame: %q", out[1])
	}
}

// indentedRowsComponent renders lines padded to the width, so the screen rows
// are as wide as the viewport.
type indentedRowsComponent struct{ lines []string }

func (c *indentedRowsComponent) Render(width int) []string {
	out := make([]string, 0, len(c.lines))
	for _, line := range c.lines {
		for VisibleWidth(line) < width {
			line += " "
		}
		out = append(out, line)
	}
	return out
}

func (c *indentedRowsComponent) Invalidate() {}

// Scrolling must not move the selection: the margin is measured over the rows the
// selection covers, not over the rows that happen to be on screen.
func TestSelectionKeepsItsMarginWhileScrolling(t *testing.T) {
	terminal := &recordingTerminal{width: 20, height: 3}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	lines := make([]string, 0, 10)
	for index := range 10 {
		if index == 4 {
			lines = append(lines, "  alpha")
			continue
		}
		lines = append(lines, "      row"+itoa(index))
	}
	screen.AddChild(NewScrollView(&indentedRowsComponent{lines: lines}, ScrollViewOptions{Primary: true}))
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	// The view the layout reports is the one the selection must name.
	var view *ScrollView
	var find func(box *LayoutBox)
	find = func(box *LayoutBox) {
		if box.ScrollView != nil {
			view = box.ScrollView
		}
		for _, child := range box.Children {
			find(child)
		}
	}
	find(screen.currentLayout.Root)
	if view == nil {
		t.Fatal("no scroll view in the layout")
	}

	// The selection covers rows 4 to 9, so its shared margin is alpha's two
	// spaces; only the last three rows are on screen.
	anchor := altSelectionPoint{Row: 4, Col: 0, ScrollView: view}
	focus := altSelectionPoint{Row: 9, Col: 20, ScrollView: view, Boundary: true}
	screen.selectionAnchor = &anchor
	screen.selectionFocus = &focus

	out := screen.applySelection(screen.previousScreen, screen.currentLayout)
	if got := selectionHighlightStart(t, out[0]); got != 2 {
		t.Fatalf("the highlight starts at %d, want the shared margin 2", got)
	}

	// Scrolling does not move it.
	view.ScrollTo(view.ScrollTop() - 1)
	screen.RenderNow(false)
	out = screen.applySelection(screen.previousScreen, screen.currentLayout)
	if got := selectionHighlightStart(t, out[1]); got != 2 {
		t.Fatalf("after scrolling the highlight starts at %d, want the shared margin 2", got)
	}
}
