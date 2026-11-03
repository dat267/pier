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

func FuzzStreamingSanitizerMatchesFull(f *testing.F) {
	for _, seed := range []string{"", "hello", "hello\x1b[31mred", "hello\x1b[31mred\x1b[0m", "\x00\x7f"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 512 {
			return
		}
		s := &StreamingSanitizer{}
		for n := 0; n <= len(source); n++ {
			got := s.Sanitize(source[:n])
			want := SanitizeUntrustedText(source[:n])
			if got != want {
				t.Fatalf("prefix %d/%d = %q, want %q", n, len(source), got, want)
			}
		}
	})
}
