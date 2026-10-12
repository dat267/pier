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
