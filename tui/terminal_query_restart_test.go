package tui

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), packages/tui/src/tui.ts TuiBase.start
// clears stopped on each start, including external-editor and suspend resumes.
func TestRendererColorQueryWorksAfterRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		name := "initial"
		if restart {
			name = "restarted"
		}
		t.Run(name, func(t *testing.T) {
			terminal := &fakeTerminal{width: 80, height: 24}
			renderer := NewRenderer(terminal)
			renderer.Start()
			if restart {
				renderer.Stop(TuiStopOptions{PreserveScreen: true})
				renderer.Start()
			}
			defer renderer.Stop(TuiStopOptions{PreserveScreen: true})
			wait, ok := renderer.BeginTerminalColors()
			if !ok {
				t.Fatal("running renderer rejected the color query")
			}
			renderer.HandleTerminalInput("\x1b]11;#010203\x07")
			renderer.HandleTerminalInput("\x1b[?62;22c")
			colors := wait(1000, nil)
			if colors.Background == nil || *colors.Background != (RgbColor{R: 1, G: 2, B: 3}) {
				t.Fatalf("color query did not resolve: %+v", colors.Background)
			}
		})
	}
}

// Reference: pi v1.5.0 (f9a26365), tui.ts TuiBase.stop disables the wire
// notification but keeps the desired setting so start re-enables it.
func TestRendererRestoresColorSchemeNotificationsAfterRestart(t *testing.T) {
	terminal := &fakeTerminal{width: 80, height: 24}
	renderer := NewRenderer(terminal)
	renderer.Start()
	renderer.SetTerminalColorSchemeNotifications(true)
	renderer.Stop(TuiStopOptions{PreserveScreen: true})
	renderer.Start()
	defer renderer.Stop(TuiStopOptions{PreserveScreen: true})
	if enables := strings.Count(terminal.joinedWrites(), "\x1b[?2031h"); enables != 2 {
		t.Fatalf("notification enables = %d, want one on each start", enables)
	}
}
