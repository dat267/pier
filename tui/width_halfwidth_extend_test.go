package tui

import "testing"

// TestSegmentGraphemesAttachesHalfwidthVoicingMarks pins A508 against the pin's
// segmentation. Reference: pi v1.5.0 (f9a26365) packages/tui/src/utils.ts
// `visibleWidth` segments with `Intl.Segmenter`, whose cluster keeps a
// halfwidth katakana voiced (U+FF9E) or semi-voiced (U+FF9F) sound mark in the
// same grapheme as its base: UCD GraphemeBreakProperty.txt lists both as
// `Extend`. Pier split them, so a base with an early width return (a regional
// indicator here) had the mark counted a second time, one cell wider than the
// pin reports.
func TestSegmentGraphemesAttachesHalfwidthVoicingMarks(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		// ハ + voiced mark: one cluster on both sides; the width is 2 either way.
		{"ﾊﾞ", []string{"ﾊﾞ"}},
		// An unpaired regional indicator followed by the mark: one cluster on
		// the pin's segmenter, so the indicator's fixed width covers it.
		{"🇯ﾞ", []string{"🇯ﾞ"}},
		{"🇯ﾟ", []string{"🇯ﾟ"}},
		// A mark that begins the text stays its own cluster.
		{"ﾞﾊ", []string{"ﾞ", "ﾊ"}},
	}
	for _, tc := range cases {
		got := segmentGraphemes(tc.text)
		if len(got) != len(tc.want) {
			t.Fatalf("segmentGraphemes(%q) = %q, want %q", tc.text, got, tc.want)
		}
		for index := range got {
			if got[index] != tc.want[index] {
				t.Fatalf("segmentGraphemes(%q) = %q, want %q", tc.text, got, tc.want)
			}
		}
	}

	// The pin's visibleWidth: 2 for the indicator cluster, 2 for ハ+mark.
	for text, want := range map[string]int{"🇯ﾞ": 2, "🇯ﾟ": 2, "ﾊﾞ": 2, "ﾊﾟ": 2} {
		if got := VisibleWidth(text); got != want {
			t.Errorf("VisibleWidth(%q) = %d, want %d", text, got, want)
		}
	}
}
