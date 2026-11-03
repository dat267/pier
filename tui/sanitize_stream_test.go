package tui

import "testing"

// The streaming fast path must agree with a full scan at every append and
// after a rewrite or a control sequence.
func TestStreamingSanitizerMatchesFullSanitize(t *testing.T) {
	s := &StreamingSanitizer{}
	texts := []string{
		"", "hello", "hello world", "hello world\n", "hello world\n\x1b[31mred",
		"hello world\n\x1b[31mred\x1b[0m", "hello world\n\x1b[31mred\x1b[0m tail",
		"rewritten text", "rewritten\x00text", "rewritten\x00text tail",
	}
	for _, text := range texts {
		got := s.Sanitize(text)
		want := SanitizeUntrustedText(text)
		if got != want {
			t.Fatalf("Sanitize(%q) = %q, want %q", text, got, want)
		}
	}
}
