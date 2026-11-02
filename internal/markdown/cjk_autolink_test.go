package markdown

import (
	"reflect"
	"testing"
)

// pi v1.5.0 markdown.ts MarkdownTokenizer.url: a bare autolink stops at the
// first position matching cjkPunctuationRegex (a CJK character followed by
// punctuation, or an explicit fullwidth mark) and the prefix is re-tokenized.
func TestBareAutolinksStopAtCJKPunctuation(t *testing.T) {
	cases := map[string][]Inline{
		// The cut falls before the CJK character whose successor is punctuation.
		"見https://example.com/テスト。次": {
			{Kind: InlineText, Text: "見"},
			{Kind: InlineLink, Text: "https://example.com/テス", Dest: "https://example.com/テス",
				Children: []Inline{{Kind: InlineText, Text: "https://example.com/テス"}}},
			{Kind: InlineText, Text: "ト。次"},
		},
		// A fullwidth mark terminates the link wherever it appears.
		"https://example.com/test，next": {
			{Kind: InlineLink, Text: "https://example.com/test", Dest: "https://example.com/test",
				Children: []Inline{{Kind: InlineText, Text: "https://example.com/test"}}},
			{Kind: InlineText, Text: "，next"},
		},
		// An ideographic full stop alone is not in the explicit set, and the
		// character before it is not CJK, so the link keeps it.
		"https://example.com/test。": {
			{Kind: InlineLink, Text: "https://example.com/test。", Dest: "https://example.com/test。",
				Children: []Inline{{Kind: InlineText, Text: "https://example.com/test。"}}},
		},
	}
	for src, want := range cases {
		if got := ParseInline(src); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q = %#v, want %#v", src, got, want)
		}
	}
}

func TestCJKPunctuationCut(t *testing.T) {
	for raw, want := range map[string]int{
		"https://example.com/test":  -1,
		"https://example.com/test。": -1, // U+3002 alone is not in the set
		"https://example.com/テスト。":  len("https://example.com/テス"),
		"https://example.com/test，": len("https://example.com/test"),
		"https://example.com/テスト":   -1,
		"https://example.com/テスト！":  len("https://example.com/テス"),
	} {
		if got := cjkAutolinkCut(raw); got != want {
			t.Fatalf("cjkAutolinkCut(%q) = %d, want %d", raw, got, want)
		}
	}
}
