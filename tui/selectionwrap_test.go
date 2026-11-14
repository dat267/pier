package tui

import (
	"testing"
)

// selectionText runs a drag from the first cell of the first row to the end of
// the last row and returns the copied text.
func selectionText(t *testing.T, width int, lines []string) string {
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

	text, ok := screen.getActiveSelectionTextLocked()
	if !ok {
		t.Fatal("no selection text")
	}
	return text
}

// D472: a row that runs to the row width was wrapped by the renderer, so the
// copy joins it with the next row. A shorter row ended with a newline.
func TestSelectionJoinsSoftWrappedRows(t *testing.T) {
	got := selectionText(t, 10, []string{"abcdefghij", "klm"})
	if want := "abcdefghijklm"; got != want {
		t.Fatalf("wrapped copy = %q, want %q", got, want)
	}
}

func TestSelectionKeepsHardLineBreaks(t *testing.T) {
	got := selectionText(t, 10, []string{"abc", "klm"})
	if want := "abc\nklm"; got != want {
		t.Fatalf("copy = %q, want %q", got, want)
	}
}

// The continuation row repeats the block's indent; joining drops it so the
// paragraph reads as one line.
func TestSelectionJoinsWrappedRowsWithoutTheirIndent(t *testing.T) {
	got := selectionText(t, 10, []string{"  abcdefgh", "  klm"})
	if want := "  abcdefghklm"; got != want {
		t.Fatalf("indented wrapped copy = %q, want %q", got, want)
	}
}
