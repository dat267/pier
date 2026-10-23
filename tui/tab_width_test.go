package tui

import "testing"

// Upstream tui/test/tab-width.test.ts keeps slice and overlay segment widths
// consistent with visibleWidth, including tabs.
func TestExtractSegmentsTabWidths(t *testing.T) {
	text := "out 192M\t.pi/skill-tests/results-ha"
	if slice := SliceWithWidth(text, 0, 10, true); slice.Text != "out 192M" || slice.Width != 8 {
		t.Fatalf("slice = %q (w=%d); want %q (w=8)", slice.Text, slice.Width, "out 192M")
	}
	segments := ExtractSegments(text, 10, 13, 10, true)
	if segments.Before != "out 192M" || segments.BeforeWidth != 8 {
		t.Fatalf("before = %q (w=%d); want %q (w=8)", segments.Before, segments.BeforeWidth, "out 192M")
	}
	if got := VisibleWidth(segments.Before); got != segments.BeforeWidth {
		t.Fatalf("before visible width = %d; want %d", got, segments.BeforeWidth)
	}
	tabFits := ExtractSegments(text, 11, 13, 10, true)
	if tabFits.Before != "out 192M\t" || tabFits.BeforeWidth != 11 {
		t.Fatalf("before = %q (w=%d); want %q (w=11)", tabFits.Before, tabFits.BeforeWidth, "out 192M\t")
	}
	if got := VisibleWidth(tabFits.Before); got != tabFits.BeforeWidth {
		t.Fatalf("tab before visible width = %d; want %d", got, tabFits.BeforeWidth)
	}
}

// Upstream keeps tabs inside terminal control sequences byte-identical while
// expanding tabs in visible text.
func TestNormalizeTerminalOutputKeepsTabsInControlSequences(t *testing.T) {
	for _, control := range []string{
		"\x1b]8;;https://example.test/a\tb\x07",
		"\x1b]0;window\ttitle\x1b\\",
		"\x1b_payload\tdata\x1b\\",
	} {
		got := NormalizeTerminalOutput(control + "label\ttext")
		want := control + "label   text"
		if got != want {
			t.Fatalf("normalize = %q; want %q", got, want)
		}
	}
}
