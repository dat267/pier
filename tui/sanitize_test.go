package tui

import "testing"

func TestSanitizeUntrustedTextRemovesTerminalControls(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text", "hello world", "hello world"},
		{"screen clear", "hello\x1b[2Jworld", "helloworld"},
		{"cursor home", "a\x1b[Hb", "ab"},
		{"sgr is untrusted too", "\x1b[31mred\x1b[0m", "red"},
		{"private mode", "x\x1b[?25ly", "xy"},
		{"osc clipboard", "a\x1b]52;c;c2VjcmV0\x07b", "ab"},
		{"osc with st", "a\x1b]8;;https://x\x1b\\b", "ab"},
		{"dcs payload", "a\x1bP1;2|payload\x1b\\b", "ab"},
		{"apc image", "a\x1b_Gpayload\x1b\\b", "ab"},
		{"charset", "a\x1b(Bb", "ab"},
		{"truncated escape", "a\x1bb", "a"},
		{"unterminated osc", "a\x1b]52;c;secret", "a"},
		{"bare bel", "a\x07b", "ab"},
		{"del", "a\x7fb", "ab"},
		{"carriage return", "a\rb", "ab"},
		{"keeps newline and tab", "a\n\tb", "a\n\tb"},
		{"keeps unicode", "héllo ✓", "héllo ✓"},
		{"drops c1 controls", "a\u009bb", "a\u009bb"},
		{"multiple sequences", "\x1b[2J\x1b[31mx\x1b]0;t\x07", "x"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := SanitizeUntrustedText(test.in); got != test.want {
				t.Fatalf("SanitizeUntrustedText(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// Ordinary text must not allocate, because this runs for every streamed
// assistant delta.
func TestSanitizeUntrustedTextLeavesPlainTextAlone(t *testing.T) {
	text := "a plain assistant sentence with unicode ✓"
	allocations := testing.AllocsPerRun(100, func() { _ = SanitizeUntrustedText(text) })
	if allocations != 0 {
		t.Fatalf("plain text allocated %v times", allocations)
	}
}
