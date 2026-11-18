package tui

// SanitizeUntrustedText is the boundary for provider- and model-supplied text
// (M37, M64): no C0 control may survive except the newline and tab the layout
// needs.

import "testing"

func FuzzSanitizeUntrustedText(f *testing.F) {
	for _, seed := range []string{"plain", "\x1b[31mred\x1b[0m", "\x1b]8;;u\x07x\x1b]8;;\x07", "\x00\x1f\x7f", "é😀"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		out := SanitizeUntrustedText(text)
		for _, r := range out {
			if r < 0x20 && r != '\n' && r != '\t' {
				t.Fatalf("control rune %q survived in %q", r, out)
			}
		}
	})
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
