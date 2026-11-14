package tui

import (
	"strings"
	"testing"
)

// D471: a single-line input draws its value directly, and a paste is the one
// path that can carry escape sequences into it. The reference's input.ts pastes
// what it is given; pier strips them on entry, as the editor does (D452), so a
// pasted string cannot write the clipboard, retitle the window or move the
// cursor.
func TestInputPasteCannotEmitTerminalControls(t *testing.T) {
	input := NewInput(InputOptions{})
	input.HandleInput("\x1b[200~" + "\x1b]52;c;aGk=\x07title\x1b[2J" + "hello" + "\x1b[201~")
	value := input.Value()
	if strings.ContainsAny(value, "\x1b\x07") {
		t.Fatalf("value = %q, want no control characters", value)
	}
	if !strings.Contains(value, "hello") {
		t.Fatalf("value = %q, want the pasted text kept", value)
	}

	// Typed text still reaches the value, so the paste path is the only change.
	input = NewInput(InputOptions{})
	input.HandleInput("abc")
	if got := input.Value(); got != "abc" {
		t.Fatalf("value = %q, want typed text", got)
	}
}
