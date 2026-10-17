package tui

import (
	"strings"
	"testing"
)

// fixedLinesComponent renders fixed lines verbatim (no padding or clamping), so
// a test can hand the screen an over-wide styled line.
type fixedLinesComponent struct{ lines []string }

func (c *fixedLinesComponent) Render(int) []string { return c.lines }
func (c *fixedLinesComponent) Invalidate()         {}

// A line the screen clamps to the terminal width must keep its trailing reset.
// The clamp used to run after ApplyLineResets, so slicing to the width dropped
// the reset (and any trailing style close) and the line's own background or
// foreground leaked into the next row the terminal wrote. That surfaced as a
// colorized line above the editor's working-status border after a resize, when
// a stale old-width transcript line was clamped.
func TestAltScreenOverWideClampKeepsLineReset(t *testing.T) {
	terminal := &recordingTerminal{width: 10, height: 3}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.AddChild(&fixedLinesComponent{lines: []string{
		"\x1b[48;2;10;20;30m" + strings.Repeat("x", 30) + "\x1b[49m",
		"next",
	}})
	screen.Start()
	screen.RenderNow(true)

	out := terminal.takeWrites()
	if !strings.Contains(out, strings.Repeat("x", 10)+"\x1b[0m") {
		t.Fatalf("clamped line lost its reset; writes:\n%q", out)
	}
}

// The deferred-document replay clamps the same way; keep its reset too.
func TestAltScreenDeferredReplayClampKeepsLineReset(t *testing.T) {
	terminal := &recordingTerminal{width: 10, height: 3}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.AddChild(&fixedLinesComponent{lines: []string{
		"\x1b[48;2;10;20;30m" + strings.Repeat("y", 30) + "\x1b[49m",
	}})
	screen.Start()
	screen.Stop(TuiStopOptions{})

	out := terminal.takeWrites()
	if !strings.Contains(out, strings.Repeat("y", 10)+"\x1b[0m") {
		t.Fatalf("replayed clamped line lost its reset; writes:\n%q", out)
	}
}
