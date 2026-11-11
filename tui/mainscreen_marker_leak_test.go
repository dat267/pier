package tui

import (
	"strings"
	"testing"
)

// pi v1.5.2 (b4afdddf) tui.ts prepareLines strips CURSOR_MARKER from every
// non-image line before writing, because a marker is internal render metadata.
// The hardware cursor row is taken from one line, so a marker on any other line,
// or on a line outside the scanned viewport, is a stale sentinel; writing it put
// a raw APC sequence in the terminal stream.
func TestScreensStripStaleCursorMarkers(t *testing.T) {
	lines := func() []string {
		values := make([]string, 6)
		for i := range values {
			values[i] = "line" + string(rune('a'+i))
		}
		// The stale marker is above a short viewport; the live one is last.
		values[1] = "stale" + CursorMarker + "row"
		values[5] = "focused" + CursorMarker + "row"
		return values
	}
	for _, height := range []int{2, 3, 6} {
		t.Run("main/"+string(rune('0'+height)), func(t *testing.T) {
			terminal := &recordingTerminal{width: 20, height: height}
			screen := NewMainScreen(terminal, true, t.TempDir())
			screen.DisableAutoRender()
			screen.AddChild(&scriptedComponent{lines: lines()})
			screen.Start()
			screen.RenderNow(false)
			if out := terminal.takeWrites(); strings.Contains(out, CursorMarker) {
				t.Fatalf("marker reached the terminal: %q", out)
			}
		})
	}

	previous := GetTerminalCapabilities()
	previousCells := GetCellDimensions()
	t.Cleanup(func() {
		SetTerminalCapabilities(previous)
		SetCellDimensions(previousCells)
	})
	SetTerminalCapabilities(TerminalCapabilities{Images: ImageProtocolKitty, TrueColor: true})
	SetCellDimensions(CellDimensions{WidthPx: 10, HeightPx: 10})
	t.Run("alt-screen", func(t *testing.T) {
		terminal := &recordingTerminal{width: 20, height: 3}
		screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
		screen.DisableAutoRender()
		screen.AddChild(&scriptedComponent{lines: lines()})
		screen.Start()
		screen.RenderNow(false)
		if out := terminal.takeWrites(); strings.Contains(out, CursorMarker) {
			t.Fatalf("marker reached the terminal: %q", out)
		}
	})
}
