package tui

import (
	"strings"
	"testing"
)

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

// The renderer pads every line to the terminal width, so a drag across the end
// of a line used to paint the padding as selected: a solid rectangle with no
// text under it. The highlight now stops at the line's last non-blank cell.
func TestSelectionHighlightSkipsHorizontalPadding(t *testing.T) {
	terminal := &recordingTerminal{width: 20, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(&scriptedComponent{lines: []string{"hello world", "b", "gamma"}})
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	// Drag from the first cell to past the end of the last line.
	screen.HandleTerminalInput("\x1b[<0;1;1M")
	screen.HandleTerminalInput("\x1b[<32;20;3M")
	screen.HandleTerminalInput("\x1b[<0;20;3m")

	out := screen.applySelection(screen.previousScreen, screen.currentLayout)
	if got := selectionHighlightWidth(t, out[0]); got != len("hello world") {
		t.Fatalf("first row highlight = %d cells, want %d", got, len("hello world"))
	}
	if got := selectionHighlightWidth(t, out[1]); got != len("b") {
		t.Fatalf("middle row highlight = %d cells, want %d", got, len("b"))
	}
}

// A row is highlighted from its first non-blank cell, so the transcript's left
// margin is not painted as part of the selection, including on the row where
// the drag started. The copied text keeps the indentation: only the highlight
// skips the blanks.
func TestSelectionHighlightSkipsLeadingPadding(t *testing.T) {
	terminal := &recordingTerminal{width: 20, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(&scriptedComponent{lines: []string{"  alpha", "  beta"}})
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	// Drag from the first cell of the first row to past the end of the second.
	screen.HandleTerminalInput("\x1b[<0;1;1M")
	screen.HandleTerminalInput("\x1b[<32;20;2M")
	screen.HandleTerminalInput("\x1b[<0;20;2m")

	out := screen.applySelection(screen.previousScreen, screen.currentLayout)
	for row, want := range []string{"alpha", "beta"} {
		if got := selectionHighlightWidth(t, out[row]); got != len(want) {
			t.Fatalf("row %d highlight = %d cells, want %d", row, got, len(want))
		}
		if !strings.Contains(StripTerminalSequences(out[row]), "  "+want) {
			t.Fatalf("row %d lost its indentation in the frame: %q", row, out[row])
		}
	}
}
