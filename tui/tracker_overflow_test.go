package tui

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) packages/tui/src/utils.ts AnsiCodeTracker.
//
// The tracker parses each SGR parameter with Number.parseInt, which returns a
// huge Number for a parameter wider than 2^53. That value matches no tracked
// code, so the sequence leaves every attribute untouched. ParseInteger wraps in
// Go instead (tui/tracker.go parseSGRNumber accumulates into an int), so a
// parameter of 2^64 wrapped to 0 and reset the whole style.
func TestTrackerIgnoresOutOfRangeSGRParameter(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"24 digits", "\x1b[31mabc\x1b[18446744073709551616mdef"},
		{"20 digits", "\x1b[31mabc\x1b[99999999999999999999mdef"},
		{"int64 max + 1", "\x1b[31mabc\x1b[9223372036854775808mdef"},
		{"32-bit wrap", "\x1b[31mabc\x1b[4294967296mdef"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			lines := WrapTextWithAnsi(test.in, 3)
			if len(lines) != 2 {
				t.Fatalf("lines = %q, want 2 lines", lines)
			}
			// The red SGR opened before the text still applies to the second
			// line: the out-of-range parameter must not reset it.
			if !strings.Contains(lines[1], "\x1b[31m") {
				t.Fatalf("second line %q lost the red foreground", lines[1])
			}
		})
	}
}

// An out-of-range underline parameter must also leave the underline state
// alone, so the wrapped line still closes it with SGR 24.
func TestTrackerKeepsUnderlineAcrossOutOfRangeSGRParameter(t *testing.T) {
	lines := WrapTextWithAnsi("\x1b[4mabc\x1b[18446744073709551616mdef", 3)
	if len(lines) != 2 {
		t.Fatalf("lines = %q, want 2 lines", lines)
	}
	if !strings.Contains(lines[0], "\x1b[24m") {
		t.Fatalf("first line %q did not close the underline", lines[0])
	}
	if !strings.Contains(lines[1], "\x1b[4m") {
		t.Fatalf("second line %q did not reopen the underline", lines[1])
	}
}
