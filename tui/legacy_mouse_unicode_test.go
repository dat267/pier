package tui

import (
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), stdin-buffer.ts counts six UTF-16 characters for
// legacy mouse reports; tui-alt-screen.ts parseWheelEvent reads coordinates
// with charCodeAt. The BMP report below is wheel-up at column200,row0.
func TestAltScreenLegacyMouseKeepsUnicodeCoordinates(t *testing.T) {
	for _, c := range []struct{ name, input string }{
		{"SGR control", "\x1b[<64;201;1M"},
		{"legacy UTF8", "\x1b[M`é!"},
	} {
		t.Run(c.name, func(t *testing.T) {
			terminal := &recordingTerminal{width: 250, height: 4}
			screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
			screen.DisableAutoRender()
			recorder := &mouseRecordingComponent{staticComponent: staticComponent{lines: []string{strings.Repeat("x", 250)}}}
			screen.AddChild(recorder)
			screen.Start()
			defer screen.Stop(TuiStopOptions{})
			screen.RenderNow(false)
			buffer := NewStdinBuffer(StdinBufferOptions{})
			defer buffer.Destroy()
			buffer.OnData = screen.HandleTerminalInput
			buffer.ProcessString(c.input)
			if got := recorder.last; got.Type != MouseWheel || got.X != 200 || got.Y != 0 {
				t.Fatalf("wheel event = %+v, want wheel at200,0", got)
			}
		})
	}
}
