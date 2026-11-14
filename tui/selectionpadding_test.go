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

// The right side keeps the row's padding: the renderer pads every row to the
// terminal width, and upstream paints the padding as selected.
func TestSelectionHighlightKeepsTheRightPadding(t *testing.T) {
	out := selectionFrame(t, 20, []string{"hello world", "b", "gamma"})
	if got := selectionHighlightWidth(t, out[0]); got != 20 {
		t.Fatalf("first row highlight = %d cells, want the full row", got)
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
