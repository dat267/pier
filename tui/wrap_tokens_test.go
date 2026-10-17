package tui

import (
	"reflect"
	"strings"
	"testing"
)

// TestSplitIntoTokensFastPath pins the token stream for printable-ASCII input
// (the substring fast path) and for styled/CJK input (the segmented path).
func TestSplitIntoTokensFastPath(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"hello world", []string{"hello", " ", "world"}},
		{"a  b", []string{"a", "  ", "b"}},
		{" a", []string{" ", "a"}},
		{"a b ", []string{"a", " ", "b", " "}},
		{"\x1b[31mred\x1b[0m plain", []string{"\x1b[31mred", "\x1b[0m ", "plain"}},
		{"ab中cd", []string{"ab", "中", "cd"}},
	}
	for _, tc := range cases {
		if got := splitIntoTokensWithAnsi(tc.text); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitIntoTokensWithAnsi(%q) = %#v; want %#v", tc.text, got, tc.want)
		}
	}
}

// TestWrapMalformedEscapeTerminates guards a wrap that used to spin forever: a
// lone ESC in a line long enough to wrap made the chunk search find the current
// (invalid) escape at offset zero and never advance.
func TestWrapMalformedEscapeTerminates(t *testing.T) {
	text := strings.Repeat("x", 5) + "\x1b" + strings.Repeat("y", 5)
	lines := WrapTextWithAnsi(text, 6)
	if joined := strings.Join(lines, ""); joined != text {
		t.Fatalf("WrapTextWithAnsi(%q, 6) = %q, want the text preserved", text, lines)
	}
}

// TestWrapAsciiAllocationBudget: a plain ASCII line must not build a per-byte
// segment slice or a per-token builder buffer. Before the fast path a 200-byte
// line allocated 106 objects; it is 26 now.
func TestWrapAsciiAllocationBudget(t *testing.T) {
	line := strings.Repeat("word ", 40)
	allocations := testing.AllocsPerRun(200, func() { _ = WrapTextWithAnsi(line, 78) })
	if allocations > 40 {
		t.Fatalf("wrapping a 200-byte ASCII line allocated %.0f objects, want at most 40", allocations)
	}
}
