package tui

import "testing"

// TestSliceKeepsAnsiOrderAcrossTheBoundary pins the order of the style carried
// into a slice and a code that applies at the slice's first column.
//
// A slice that begins exactly where a styled span ends carries the span's colour
// in from before the range and meets its reset at the range's first column. The
// pair has to stay in that order. Writing the in-range code as it is met and
// flushing the carried prefix only when the first text is written swaps them, so
// the reset lands ahead of the colour it cancels and the sliced text renders in
// the style that had just ended — a double-clicked word inside inline code left
// the rest of the line yellow.
func TestSliceKeepsAnsiOrderAcrossTheBoundary(t *testing.T) {
	line := "\x1b[33mabcdef\x1b[39m tail"
	got := SliceByColumn(line, 6, 5, true)
	want := "\x1b[33m\x1b[39m tail"
	if got != want {
		t.Fatalf("slice kept the wrong order:\n got %q\nwant %q", got, want)
	}
}

// TestSliceDropsAPCMarkersFromBeforeTheRange pins the one-shot rule for APC
// sequences in a slice.
//
// Reference: pi v1.5.2 (b4afdddf) packages/tui/src/utils.ts sliceWithWidth. Its
// pending-code branch is `currentCol < startCol && !ansi.code.startsWith("\x1b_")`:
// an APC sequence (the cursor marker, a kitty command) is a positional one-shot
// command, not persistent style, so a marker before the range must not be
// carried into the slice, while a marker inside the range stays where it is.
func TestSliceDropsAPCMarkersFromBeforeTheRange(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		start int
		len   int
		want  string
	}{
		{"marker before the range", "\x1b_apc\x1b\\text", 1, 2, "ex"},
		{"marker at the range start", "a\x1b_mk\x1b\\bc", 1, 2, "\x1b_mk\x1b\\bc"},
		{"style before the range still carries", "\x1b[31mred", 1, 2, "\x1b[31med"},
		{"kitty command before the range", "\x1b_Ga=T,i=7;AAAA\x1b\\abcdef", 2, 2, "cd"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SliceByColumn(testCase.line, testCase.start, testCase.len, false); got != testCase.want {
				t.Fatalf("slice = %q, want %q", got, testCase.want)
			}
		})
	}
}
