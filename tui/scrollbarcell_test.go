package tui

import (
	"runtime"
	"strings"
	"testing"
)

// TestReplaceScrollbarCellGolden characterizes the one-cell scrollbar overlay:
// the target grapheme is replaced while its active background (and the ANSI
// state around it) is preserved. The previous implementation sliced the line
// three times with full grapheme segmentation; the allocation-free rewrite must
// reproduce these bytes exactly.
func TestReplaceScrollbarCellGolden(t *testing.T) {
	const reset = "\x1b[0m\x1b]8;;\x07"
	cases := []struct {
		name     string
		line     string
		column   int
		width    int
		repl     string
		preserve bool
		want     string
	}{
		{"plain", "hello world", 5, 11, "X", true, "hello" + reset + "Xworld"},
		{"styled middle", "\x1b[31mhello\x1b[0m", 3, 5, "\x1b[37mY\x1b[39m", true, "\x1b[31mhel" + reset + "\x1b[37mY\x1b[39m\x1b[31mo"},
		{"styled first", "\x1b[31mhello\x1b[0m", 0, 5, "\x1b[37mY\x1b[39m", true, reset + "\x1b[37mY\x1b[39m\x1b[31mello"},
		{"styled last", "\x1b[31mhello\x1b[0m", 4, 5, "\x1b[37mY\x1b[39m", true, "\x1b[31mhell" + reset + "\x1b[37mY\x1b[39m"},
		{"wide inside", "日本語abc", 1, 9, "X", true, reset + " X本語abc"},
		{"wide second", "日本語abc", 2, 9, "X", true, "日" + reset + "X 語abc"},
		{"wide first", "日本語abc", 0, 9, "X", true, reset + "X 本語abc"},
		{"past end", "hi", 5, 10, "X", true, "hi   " + reset + "X"},
		{"last cell", "hi", 1, 10, "X", true, "h" + reset + "X"},
		{"bg first", "\x1b[44ma\x1b[0mb", 0, 2, "Z", true, reset + "\x1b[44mZ\x1b[44m\x1b[0mb"},
		{"bg second", "\x1b[44ma\x1b[0mb", 1, 2, "Z", true, "\x1b[44ma" + reset + "Z"},
		{"bg not preserved", "\x1b[44ma\x1b[0mb", 0, 2, "Z", false, reset + "Z\x1b[44m\x1b[0mb"},
		{"tab", "a\tb", 1, 3, "X", true, "a" + reset + "X  "},
		{"hyperlink", "\x1b]8;;http://x\x07link\x1b]8;;\x07", 1, 4, "X", true, "\x1b]8;;http://x\x07l" + reset + "X\x1b]8;;http://x\x07nk"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := replaceScrollbarCell(c.line, c.column, c.width, c.repl, c.preserve); got != c.want {
				t.Fatalf("replaceScrollbarCell(%q, %d) = %q, want %q", c.line, c.column, got, c.want)
			}
		})
	}
}

// allocBytesPerRun reports the bytes allocated per call of fn (the object count
// hides the win here: the old path allocated a []string of one header per
// character).
func allocBytesPerRun(runs int, fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range runs {
		fn()
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(runs)
}

// TestReplaceScrollbarCellAllocationBudget pins the scrollbar repaint cost. The
// transient scrollbar is repainted on every wheel frame, and the old path
// segmented each visible line into a []string three times (segmentGraphemes was
// ~60% of the frame's allocated bytes). The byte volume must stay small.
func TestReplaceScrollbarCellAllocationBudget(t *testing.T) {
	line := "\x1b[3m" + strings.Repeat("a line of reasoning that wraps the viewport ", 4) + "\x1b[0m"
	const repl = "\x1b[37m┃\x1b[39m"
	bytes := allocBytesPerRun(500, func() {
		replaceScrollbarCell(line, 40, VisibleWidth(line), repl, true)
	})
	if bytes > 2048 {
		t.Fatalf("replaceScrollbarCell allocated %d bytes/call, want <= 2048", bytes)
	}
	t.Logf("replaceScrollbarCell: %d bytes/call", bytes)
}
