package tui

import (
	"reflect"
	"testing"
)

// Upstream's splitIntoTokensWithAnsi breaks only on cjkBreakRegex (CJK-script
// letters and CJK punctuation with those script extensions), not on fullwidth
// forms or general punctuation.
func TestWrapTokensDoNotBreakAtFullwidthPunctuation(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"a，b—c", []string{"a，b—c"}},
		{"a。b", []string{"a", "。", "b"}},
		{"a、b", []string{"a", "、", "b"}},
	}
	for _, testCase := range cases {
		if got := splitIntoTokensWithAnsi(testCase.text); !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("splitIntoTokensWithAnsi(%q) = %#v; want %#v", testCase.text, got, testCase.want)
		}
	}
}

// The embedded cjkBreakRanges match upstream's cjkBreakRegex
// (Script_Extensions CJK), which is narrower than a script class.
func TestCJKBreakRangesMatchReference(t *testing.T) {
	breakRunes := []rune{'。', '、', '「', '」', '中', 'あ', '한', 0x1100, 0xFF61, 0x20000}
	for _, r := range breakRunes {
		if !isCJKBreakRune(r) {
			t.Errorf("isCJKBreakRune(%U) = false; want true", r)
		}
	}
	nonBreakRunes := []rune{'，', '．', '—', '…', '.', 'a', 0x3000, 0x2E9A}
	for _, r := range nonBreakRunes {
		if isCJKBreakRune(r) {
			t.Errorf("isCJKBreakRune(%U) = true; want false", r)
		}
	}
	if !containsCJKBreak("ab中") || containsCJKBreak("ab，") {
		t.Error("containsCJKBreak did not match the reference")
	}
}

// Upstream's markCharRegex is `\p{Mark}`; the custom combining table alone
// missed many Unicode marks.
func TestCombiningMarkCoversUnicodeMarks(t *testing.T) {
	for _, r := range []rune{0x07FD, 0x0897, 0x09FE, 0x0AFA, 0x1E6F5, 0x0300} {
		if !isCombiningMark(r) {
			t.Errorf("isCombiningMark(%U) = false; want true (\\p{Mark})", r)
		}
	}
}

// A lone spacing mark occupies one cell upstream (terminalSpacingMarkRegex);
// non-spacing marks and the exceptions occupy none.
func TestLoneSpacingMarkWidth(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"\u093E", 1},
		{"\u065F", 1},
		{"\u102B", 1},
		{"\u1734", 0},
		{"\u302E", 0},
		{"\u0300", 0},
	}
	for _, testCase := range cases {
		if got := VisibleWidth(testCase.text); got != testCase.want {
			t.Errorf("VisibleWidth(%q) = %d; want %d", testCase.text, got, testCase.want)
		}
	}
}

// Default_Ignorable code points are zero-width, and halfwidth voiced sound
// marks are visible after a base (upstream zeroWidthRegex/nonPrintingCharRegex).
func TestZeroWidthAndHalfwidthMarks(t *testing.T) {
	for _, text := range []string{"\u061C", "\u180E", "\u3164", "\uFFA0", "\U000E0001"} {
		if got := VisibleWidth(text); got != 0 {
			t.Errorf("VisibleWidth(%q) = %d; want 0", text, got)
		}
	}
	for _, text := range []string{"a\uFF9E", "a\uFF9F"} {
		if got := VisibleWidth(text); got != 2 {
			t.Errorf("VisibleWidth(%q) = %d; want 2", text, got)
		}
	}
}
