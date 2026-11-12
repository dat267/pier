package markdown

import (
	"reflect"
	"testing"
)

// pi v1.5.2 (b4afdddf) packages/tui/src/utils.ts cjkPunctuationRegex and
// components/markdown.ts MarkdownTokenizer.url: a bare autolink is cut at the
// first character that is itself punctuation and CJK-scripted (whose
// Script_Extensions include a CJK script, e.g. 。、」・), or at one of the
// explicit fullwidth marks. The cut keeps every character before that one.
func TestBareAutolinksStopAtCJKPunctuation(t *testing.T) {
	cases := map[string][]Inline{
		// The cut is at 。, so the Japanese path character before it stays linked.
		"見https://example.com/テスト。次": {
			{Kind: InlineText, Text: "見"},
			{Kind: InlineLink, Text: "https://example.com/テスト", Dest: "https://example.com/テスト",
				Children: []Inline{{Kind: InlineText, Text: "https://example.com/テスト"}}},
			{Kind: InlineText, Text: "。次"},
		},
		// A fullwidth mark terminates the link wherever it appears.
		"https://example.com/test，next": {
			{Kind: InlineLink, Text: "https://example.com/test", Dest: "https://example.com/test",
				Children: []Inline{{Kind: InlineText, Text: "https://example.com/test"}}},
			{Kind: InlineText, Text: "，next"},
		},
		// 。 and 、 carry a CJK script extension of their own, so they end the
		// link even after a Latin character.
		"https://example.com/test。": {
			{Kind: InlineLink, Text: "https://example.com/test", Dest: "https://example.com/test",
				Children: []Inline{{Kind: InlineText, Text: "https://example.com/test"}}},
			{Kind: InlineText, Text: "。"},
		},
		"https://example.com/test、x": {
			{Kind: InlineLink, Text: "https://example.com/test", Dest: "https://example.com/test",
				Children: []Inline{{Kind: InlineText, Text: "https://example.com/test"}}},
			{Kind: InlineText, Text: "、x"},
		},
		// Brackets and the middle dot end it the same way.
		"https://x.com/テスト」": {
			{Kind: InlineLink, Text: "https://x.com/テスト", Dest: "https://x.com/テスト",
				Children: []Inline{{Kind: InlineText, Text: "https://x.com/テスト"}}},
			{Kind: InlineText, Text: "」"},
		},
		"https://x.com/a・b": {
			{Kind: InlineLink, Text: "https://x.com/a", Dest: "https://x.com/a",
				Children: []Inline{{Kind: InlineText, Text: "https://x.com/a"}}},
			{Kind: InlineText, Text: "・b"},
		},
		"https://x.com/a｡b": {
			{Kind: InlineLink, Text: "https://x.com/a", Dest: "https://x.com/a",
				Children: []Inline{{Kind: InlineText, Text: "https://x.com/a"}}},
			{Kind: InlineText, Text: "｡b"},
		},
		// A link that would be left without a host is plain text.
		"https://。x": {{Kind: InlineText, Text: "https://。x"}},
	}
	for src, want := range cases {
		if got := ParseInline(src); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q = %#v, want %#v", src, got, want)
		}
	}
}

func TestCJKPunctuationCut(t *testing.T) {
	for raw, want := range map[string]int{
		"https://example.com/test":   -1,
		"https://example.com/test。":  len("https://example.com/test"),
		"https://example.com/テスト。":   len("https://example.com/テスト"),
		"https://example.com/test，":  len("https://example.com/test"),
		"https://example.com/テスト":    -1,
		"https://example.com/テスト！":   len("https://example.com/テスト"),
		"https://example.com/テスト」":   len("https://example.com/テスト"),
		"https://example.com/a・b":    len("https://example.com/a"),
		"https://example.com/テストー한글": -1,
	} {
		if got := cjkAutolinkCut(raw); got != want {
			t.Fatalf("cjkAutolinkCut(%q) = %d, want %d", raw, got, want)
		}
	}
}
