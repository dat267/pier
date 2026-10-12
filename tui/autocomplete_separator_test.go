package tui

import "testing"

// Upstream's cjkPunctuationRegex matches punctuation that is itself a
// CJK-script character, or one of the explicit list; the lookahead does not
// involve the next character.
func TestAutocompleteSeparatorMatchesCJKPunctuation(t *testing.T) {
	cases := []struct {
		char rune
		want bool
	}{
		{'。', true},
		{'、', true},
		{'「', true},
		{'」', true},
		{'，', true},
		{'．', true},
		{' ', true},
		{'\t', true},
		{'.', false},
		{'中', false},
		{'a', false},
	}
	for _, testCase := range cases {
		if got := isAutocompleteSeparator(testCase.char); got != testCase.want {
			t.Errorf("isAutocompleteSeparator(%q) = %v; want %v", testCase.char, got, testCase.want)
		}
	}
}

func TestAutocompleteTokenBoundaryCJKPunctuation(t *testing.T) {
	// ASCII punctuation before a CJK character is not a boundary.
	if got := findLastDelimiter("a.中"); got != -1 {
		t.Errorf("findLastDelimiter(%q) = %d; want -1", "a.中", got)
	}
	// A trailing CJK punctuation character is a boundary.
	if !tokenStartMatches("a。") {
		t.Error("tokenStartMatches(\"a。\") = false; want true")
	}
	if tokenStartMatches("a中") {
		t.Error("tokenStartMatches(\"a中\") = true; want false")
	}
}
