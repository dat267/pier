package tui

import (
	"strings"
	"testing"
	"unicode"
)

// TestSegmentGraphemesMatchesUAX29 covers the cluster boundaries upstream gets
// from Intl.Segmenter: Hangul jamo runs join, a ZWJ only joins an emoji when an
// extended pictograph precedes it, regional indicators pair up, and controls
// break on both sides. Each expectation was read off the reference
// (`new Intl.Segmenter(undefined, {granularity:"grapheme"})`).
func TestSegmentGraphemesMatchesUAX29(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"hangul syllable + trailing jamo", "\uba60\u11d0", []string{"\uba60\u11d0"}},
		{"hangul lead + vowel", "\u1100\u1160", []string{"\u1100\u1160"}},
		{"hangul lv + trailing jamo", "\uac00\u11a8", []string{"\uac00\u11a8"}},
		{"hangul trailing jamo run", "\u11a8\u11a8", []string{"\u11a8\u11a8"}},
		{"hangul lead + vowel + trailing", "\u1100\u1160\u11a8", []string{"\u1100\u1160\u11a8"}},
		{"zwj between non-emoji bases", "\u70b8\u200d\u30d0", []string{"\u70b8\u200d", "\u30d0"}},
		{"zwj emoji sequence", "\U0001F916\u200d\U0001F680", []string{"\U0001F916\u200d\U0001F680"}},
		{"zwj followed by a non-emoji", "\U0001F916\u200d\uff71", []string{"\U0001F916\u200d", "\uff71"}},
		{"heart zwj pizza", "\u2764\ufe0f\u200d\U0001F355", []string{"\u2764\ufe0f\u200d\U0001F355"}},
		{"regional indicators pair", "\U0001F1E6\U0001F1E7\U0001F1E8", []string{"\U0001F1E6\U0001F1E7", "\U0001F1E8"}},
		{"keycap base followed by zwj", "1\u200d\U0001F31B", []string{"1\u200d", "\U0001F31B"}},
		{"indic linker joins a consonant", "\u0915\u094d\u0937\u094d\u091f", []string{"\u0915\u094d\u0937\u094d\u091f"}},
		{"indic linker before a digit breaks", "\u0915\u094d1", []string{"\u0915\u094d", "1"}},
		{"thai virama is not a linker", "\u0e01\u0e4c\u0e01", []string{"\u0e01\u0e4c", "\u0e01"}},
		{"tamil virama is not a linker", "\u0b95\u0bcd\u0bb7", []string{"\u0b95\u0bcd", "\u0bb7"}},
		{"assigned tag joins its emoji", "\U0001F31B\U000E0030", []string{"\U0001F31B\U000E0030"}},
		{"unassigned tag block code point breaks", "\U000E0003\u0940", []string{"\U000E0003", "\u0940"}},
		{"control then mark", "\u0001\u0301", []string{"\u0001", "\u0301"}},
		{"carriage return then mark", "\r\u0301", []string{"\r", "\u0301"}},
	}
	for _, testCase := range cases {
		got := segmentGraphemes(testCase.input)
		if strings.Join(got, "|") != strings.Join(testCase.want, "|") {
			t.Errorf("%s: segmentGraphemes(%q) = %q, want %q", testCase.name, testCase.input, got, testCase.want)
		}
	}
}

// TestGraphemeControlRanges checks the generated Control table against the
// Unicode predicates it replaces, for every code point.
func TestGraphemeControlRanges(t *testing.T) {
	for r := rune(0); r <= 0x10FFFF; r++ {
		want := false
		switch {
		case isPrepend(r) || r == 0x200C || r == 0x200D:
		case r >= 0xE0000 && r <= 0xE0FFF:
			want = !isGraphemeExtend(r)
		default:
			want = unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) ||
				unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
		}
		if got := isGraphemeControl(r); got != want {
			t.Fatalf("isGraphemeControl(U+%04X) = %v, want %v", r, got, want)
		}
	}
}

// TestVisibleWidthMatchesGraphemeGrouping covers the widths those boundaries
// produce; they also matched the reference's visibleWidth.
func TestVisibleWidthMatchesGraphemeGrouping(t *testing.T) {
	cases := map[string]int{
		"\uba60\u11d0":                   2,
		"\u1100\u1160\u11a8":             2,
		"\u11a8\u11a8":                   1,
		"\u70b8\u200d\u30d0":             4,
		"\U0001F916\u200d\uff71":         3,
		"\U0001F1E6\U0001F1E7\U0001F1E8": 4,
		"\u0600a":                        1,
		"\uac00\u11a8":                   2,
		"\U0001F468\u200d\U0001F469\u200d\U0001F467": 2,
	}
	for input, want := range cases {
		if got := VisibleWidth(input); got != want {
			t.Errorf("VisibleWidth(%q) = %d, want %d", input, got, want)
		}
		resetWidthCacheForTest()
	}
}
