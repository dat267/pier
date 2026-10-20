package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzGraphemeWidth checks the invariants the paint path relies on for arbitrary
// text: measurement never panics or goes negative, and the per-cluster sum is
// the whole string's width.
func FuzzGraphemeWidth(f *testing.F) {
	seeds := []string{
		"", "hello", "caf\u00e9", "\u0915\u094d\u0937", "\U0001F468\u200d\U0001F469\u200d\U0001F467",
		"\U0001F1E9\U0001F1EA", "\U0001F1E6\U0001F1E7\U0001F1E8", "\u1100\u1160\u11a8", "\ufe0f\ufe0e",
		"\u094d\u0d4e\u261d", "\U000E0003\u0940", "\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F",
		"a\u0301\u200d\U0001F600", "\r\n\u0301", "\u0e33\u0eb3", "1\ufe0f\u20e3",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if !utf8.ValidString(text) {
			return
		}
		width := VisibleWidth(text)
		if width < 0 {
			t.Fatalf("VisibleWidth(%q) = %d", text, width)
		}
		// Tabs and escape sequences are normalized before segmentation, so the
		// per-cluster sum only matches the whole-string width without them.
		if strings.ContainsAny(text, "\t\x1b") {
			return
		}
		sum := 0
		for _, segment := range segmentGraphemes(text) {
			if segment == "" {
				t.Fatalf("segmentGraphemes(%q) produced an empty cluster", text)
			}
			segmentWidth := graphemeWidth(segment)
			if segmentWidth < 0 {
				t.Fatalf("graphemeWidth(%q) = %d", segment, segmentWidth)
			}
			sum += segmentWidth
		}
		if got := VisibleWidth(text); got != sum {
			t.Fatalf("VisibleWidth(%q) = %d, per-cluster sum = %d", text, got, sum)
		}
	})
}
