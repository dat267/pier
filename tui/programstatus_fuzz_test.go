package tui

// The program-status report embeds an arbitrary message in OSC 7501: the
// framing must hold and no raw C0 control may reach the terminal except the
// OSC introducer.

import (
	"strings"
	"testing"
)

func FuzzFormatProgramStatus(f *testing.F) {
	for _, seed := range []string{"", "done", "\x1b[2J", strings.Repeat("x", 5000), "é😀\n\t"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, message string) {
		status := FormatProgramStatus(ProgramStatus{State: ProgramStatusWorking, Message: message})
		if !strings.HasPrefix(status, "\x1b]7501;") || !strings.HasSuffix(status, "\x1b\\") {
			t.Fatalf("status framing = %q", status)
		}
		for _, r := range status {
			if r < 0x20 && r != 0x1b {
				t.Fatalf("raw control %q in %q", r, status)
			}
		}
	})
}
