package tui

import (
	"strings"
	"testing"
)

// TestWrapTextTrimsJavaScriptWhitespace covers upstream's `line.trimEnd()`: it
// removes every Unicode White_Space code point, not just ASCII spaces, so a
// wrapped non-breaking or ideographic space does not leave an invisible cell.
// Expectations read off the reference.
func TestWrapTextTrimsJavaScriptWhitespace(t *testing.T) {
	cases := []struct {
		text  string
		width int
		want  string
	}{
		{"\u00a0ccc", 1, "|c|c|c"},
		{"a\u00a0b", 1, "a||b"},
		{"\u00a0a", 1, "|a"},
		{"a\u3000", 1, "a|"},
		{"a\u2003", 1, "a|"},
		{"a\u200b", 1, "a\u200b"},
	}
	for _, testCase := range cases {
		got := strings.Join(WrapTextWithAnsi(testCase.text, testCase.width), "|")
		if got != testCase.want {
			t.Errorf("WrapTextWithAnsi(%q, %d) = %q, want %q", testCase.text, testCase.width, got, testCase.want)
		}
	}
}

// TestWrapLongWordClosesHyperlink covers upstream's breakLongWord: a long word is
// broken piece by piece, and each piece closes the active OSC 8 hyperlink so the
// next piece can reopen it on the following line.
func TestWrapLongWordClosesHyperlink(t *testing.T) {
	open := "\x1b]8;;https://x\x07"
	close := "\x1b]8;;\x07"
	lines := WrapTextWithAnsi(open+"world", 2)
	want := []string{open + "wo" + close, open + "rl" + close, open + "d"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}
