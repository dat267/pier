package tui

import "testing"

// pi v1.4.0 (4a080252), packages/tui/src/terminal-image.ts hyperlink: both the
// opening and closing OSC 8 sequences always end with ST (`ESC \`), regardless
// of the link text. The port switched to BEL when the text contained one,
// which changed the emitted bytes for that reachable input.
func TestHyperlinkAlwaysTerminatesWithST(t *testing.T) {
	cases := []struct {
		name, text, url string
	}{
		{"plain", "link", "https://example.com"},
		{"bel in text", "a\x07b", "https://example.com"},
		{"only bel", "\x07", ""},
		{"unicode", "héllo 漢", "https://例え.jp/漢"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			want := "\x1b]8;;" + test.url + "\x1b\\" + test.text + "\x1b]8;;\x1b\\"
			if got := Hyperlink(test.text, test.url); got != want {
				t.Fatalf("Hyperlink = %q; want %q", got, want)
			}
		})
	}
}
