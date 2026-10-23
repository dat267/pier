package tui

import (
	"strings"
	"testing"
)

// Upstream v1.4.0 9df819de3 (issue #11170): an exit path that never calls Stop
// left mouse tracking, focus reporting, bracketed paste, the Kitty keyboard
// protocol and the alternate screen enabled, so the shell prompt filled with
// escape sequences. RestoreOnExit writes those resets synchronously.

// restoreRecordingTerminal records what the renderer asks the terminal to
// restore.
type restoreRecordingTerminal struct {
	fakeTerminal
	before  string
	after   string
	restore int
}

func (c *restoreRecordingTerminal) RestoreOnExit(before, after string) {
	c.restore++
	c.before, c.after = before, after
}

// NewRenderer takes the terminal and nothing else.
func TestRendererRestoreOnExitPrefixAndSuffix(t *testing.T) {
	terminal := &restoreRecordingTerminal{}
	renderer := NewRenderer(terminal)
	renderer.RestoreOnExit()
	if terminal.restore != 1 {
		t.Fatalf("restores = %d; want 1", terminal.restore)
	}
	// The cursor is shown again, and color-scheme notifications are off when they
	// were never enabled.
	if terminal.before != "\x1b[?25h" {
		t.Fatalf("prefix = %q; want the cursor shown", terminal.before)
	}
	if terminal.after != "" {
		t.Fatalf("suffix = %q; want nothing for the main screen", terminal.after)
	}

	renderer.SetTerminalColorSchemeNotifications(true)
	renderer.RestoreOnExit()
	if terminal.before != "\x1b[?2031l\x1b[?25h" {
		t.Fatalf("prefix = %q; want the notification toggle before the cursor", terminal.before)
	}
}

func TestRendererRestoreOnExitSkipsAStoppedRenderer(t *testing.T) {
	terminal := &restoreRecordingTerminal{}
	renderer := NewRenderer(terminal)
	renderer.stopped.Store(true)
	renderer.RestoreOnExit()
	if terminal.restore != 0 {
		t.Fatalf("restores = %d; want none after Stop", terminal.restore)
	}
}

// The alternating-screen renderer restores the mode it owns, and the terminal
// pops the Kitty keyboard flags before leaving the alternate screen because
// those flags are per screen.
func TestAltScreenRestoreOnExitSequence(t *testing.T) {
	terminal := NewProcessTerminal(nil, nil)
	var output strings.Builder
	terminal.writeFn = func(data string) { output.WriteString(data) }
	screen := NewAltScreen(terminal, true, "", AltScreenOptions{})
	screen.altScreenActive = true
	terminal.keyboardProtocolPushed.Store(true)
	terminal.modifyOtherKeysActive.Store(true)

	screen.RestoreOnExit()
	written := output.String()
	screen.SetTerminalColorSchemeNotifications(true)
	screen.RestoreOnExit()
	written = output.String()
	for _, sequence := range []string{
		"\x1b[?2026l", // end synchronized output
		disableMouse,
		enableAutowrap,
		"\x1b[?2031l", // notifications were on, so they are turned off
		"\x1b[?25h",
		"\x1b[?2004l", // bracketed paste
		"\x1b[<u",     // Kitty keyboard pop
		"\x1b[>4;0m",  // modifyOtherKeys off
		exitAltScreen,
	} {
		if !strings.Contains(written, sequence) {
			t.Fatalf("restore = %q; missing %q", written, sequence)
		}
	}
	if strings.Index(written, "\x1b[<u") > strings.Index(written, exitAltScreen) {
		t.Fatalf("Kitty pop must precede leaving the alternate screen: %q", written)
	}
	if !strings.HasSuffix(written, exitAltScreen+"\x1b[?25h") {
		t.Fatalf("restore must end by leaving the alternate screen and showing the cursor: %q", written)
	}
}
