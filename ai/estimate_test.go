package ai

import (
	"strings"
	"testing"
)

// charsPerToken fell from 4 to 3.5 in v1.1.0 (#10497), so the same text estimates a
// little higher and the request is given correspondingly less room for its reply. The
// rounding is upstream's Math.ceil(chars / CHARS_PER_TOKEN); the message, content and
// tool estimators all divide by the same ratio.
func TestTextTokenEstimateUsesThreeAndAHalfCharsPerToken(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{name: "empty", text: "", want: 0},
		{name: "one char", text: "a", want: 1},
		{name: "just under a token", text: "abc", want: 1},
		{name: "just over a token", text: "abcd", want: 2},
		{name: "exactly ten tokens", text: strings.Repeat("x", 35), want: 10},
		{name: "one char past ten", text: strings.Repeat("x", 36), want: 11},
		{name: "three runes", text: "日本語", want: 1},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := EstimateTextTokens(testCase.text); got != testCase.want {
				t.Fatalf("EstimateTextTokens(%q) = %d, want %d", testCase.text, got, testCase.want)
			}
		})
	}
}
