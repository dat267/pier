package tui

// Invariants for the column slicers, which feed selection, overlay compositing
// and row clipping. Upstream (packages/tui/src/utils.ts sliceWithWidth) defines
// the same bounds; these hold for any input, so they are fuzzed rather than
// enumerated.

import (
	"testing"
)

func TestSliceInvariants(t *testing.T) {
	lines := []string{
		"abcdef",
		"\x1b[31mred\x1b[0m text",
		"\x1b]8;;https://a\x1b\\link\x1b]8;;\x1b\\",
		"\x1b_apc\x1b\\text",
		"a\tb",
		"日本語のテキスト",
		"😀😀",
		"e\u0301x",
		"",
		"\x1b[",
	}
	for _, line := range lines {
		checkSliceInvariants(t, line)
	}
}

func FuzzSliceInvariants(f *testing.F) {
	for _, seed := range []string{"", "abc", "\x1b[31mred\x1b[0m", "\x1b_apc\x1b\\text", "日本語", "😀", "a\tb", "\x1b]8;;u\x1b\\x\x1b]8;;\x1b\\"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		checkSliceInvariants(t, line)
	})
}

func checkSliceInvariants(t *testing.T, line string) {
	t.Helper()
	width := VisibleWidth(line)
	if width < 0 {
		t.Fatalf("VisibleWidth(%q) = %d", line, width)
	}
	// An empty range yields nothing, whatever the line holds.
	if got := SliceWithWidth(line, 0, 0, true); got.Text != "" || got.Width != 0 {
		t.Fatalf("empty slice of %q = %+v", line, got)
	}
	// A strict slice never exceeds the requested width.
	limit := min(width, 12)
	for start := 0; start <= limit; start++ {
		for length := 1; start+length <= limit; length++ {
			got := SliceWithWidth(line, start, length, true)
			if got.Width > length {
				t.Fatalf("strict slice of %q [%d,+%d) has width %d, text %q", line, start, length, got.Width, got.Text)
			}
			if got.Width != VisibleWidth(got.Text) {
				t.Fatalf("slice of %q [%d,+%d) reports width %d but renders %d: %q", line, start, length, got.Width, VisibleWidth(got.Text), got.Text)
			}
		}
	}
	// The whole line as one strict slice keeps its text and width.
	whole := SliceWithWidth(line, 0, width, true)
	if width > 0 && whole.Width != width {
		t.Fatalf("whole slice of %q has width %d, want %d (%q)", line, whole.Width, width, whole.Text)
	}
}
