package markdown

import (
	"reflect"
	"testing"
)

// pi v1.4.0 (4a080252), packages/tui/src/components/markdown.ts uses Marked
// 18.0.11 with marked-cjk-friendly 0.1.2. A failed run can leave an opener
// inside the run; Marked's text scanner does not consume it all at once.
func TestEmphasisOpensInsideUnmatchedRun(t *testing.T) {
	want := []Inline{
		{Kind: InlineText, Text: "：_"},
		{Kind: InlineEmphasis, Text: "1", Children: []Inline{{Kind: InlineText, Text: "1"}}},
	}
	if got := ParseInline("：__1_"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// Marked retains the last non-underscore text character as prevChar while
// retrying inside an unmatched underscore run (Lexer.inlineTokens).
func TestEmphasisRetainsTextPredecessorAcrossUnderscores(t *testing.T) {
	want := []Inline{
		{Kind: InlineText, Text: "。__"},
		{Kind: InlineEmphasis, Text: "**", Children: []Inline{{Kind: InlineText, Text: "**"}}},
		{Kind: InlineText, Text: "中"},
	}
	if got := ParseInline("。___**_中"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// CJK letters still participate in Marked's underscore word-character guard,
// before the CJK-friendly punctuation overrides are considered.
func TestEmphasisUnderscoreDoesNotOpenAfterCJKWord(t *testing.T) {
	want := []Inline{{Kind: InlineText, Text: "中___1_"}}
	if got := ParseInline("中___1_"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEmphasisDoesNotOpenBeforeSpaceAfterCJK(t *testing.T) {
	want := []Inline{{Kind: InlineText, Text: "。* x*"}}
	if got := ParseInline("。* x*"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// Marked passes the last UTF-16 code unit as prevChar. The CJK extension
// recovers supplementary CJK characters, not arbitrary supplementary symbols.
func TestEmphasisPunctuationOpenerAfterSupplementarySymbol(t *testing.T) {
	want := []Inline{{Kind: InlineText, Text: "a😀__(：__"}}
	if got := ParseInline("a😀__(：__"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEmphasisWordOpenerAfterSupplementarySymbol(t *testing.T) {
	want := []Inline{
		{Kind: InlineText, Text: "😀"},
		{Kind: InlineEmphasis, Text: "__x__", Children: []Inline{
			{Kind: InlineStrong, Text: "x", Children: []Inline{{Kind: InlineText, Text: "x"}}},
		}},
	}
	if got := ParseInline("😀___x___"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEmphasisSkipsCloserInsideOppositeMarkerPair(t *testing.T) {
	want := []Inline{{Kind: InlineText, Text: "*a__1*__"}}
	if got := ParseInline("*a__1*__"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEmphasisCountsNestedUnderscoreOpener(t *testing.T) {
	want := []Inline{
		{Kind: InlineText, Text: "_a "},
		{Kind: InlineStrong, Text: "b", Children: []Inline{{Kind: InlineText, Text: "b"}}},
	}
	if got := ParseInline("_a __b__"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEmphasisUnderscoreAfterSupplementaryCJK(t *testing.T) {
	want := []Inline{
		{Kind: InlineText, Text: "𠀀"},
		{Kind: InlineEmphasis, Text: "x", Children: []Inline{{Kind: InlineText, Text: "x"}}},
	}
	if got := ParseInline("𠀀_x_"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEmphasisRecoversCJKBeforeVariationSelector(t *testing.T) {
	for _, prefix := range []string{"中\ufe0e", "中\U000e0100"} {
		want := []Inline{
			{Kind: InlineText, Text: prefix},
			{Kind: InlineEmphasis, Text: ")", Children: []Inline{{Kind: InlineText, Text: ")"}}},
		}
		if got := ParseInline(prefix + "_)_"); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: got %#v, want %#v", prefix, got, want)
		}
	}
}

func TestEmphasisUsesJavaScriptWhitespace(t *testing.T) {
	cases := []struct {
		src  string
		want []Inline
	}{
		{"*\ufeffx*", []Inline{{Kind: InlineText, Text: "*\ufeffx*"}}},
		{"*\u0085x*", []Inline{{Kind: InlineEmphasis, Text: "\u0085x", Children: []Inline{{Kind: InlineText, Text: "\u0085x"}}}}},
	}
	for _, tc := range cases {
		if got := ParseInline(tc.src); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: got %#v, want %#v", tc.src, got, tc.want)
		}
	}
}

func TestEmphasisDoesNotReuseSupplementaryCJKInsideFailedRun(t *testing.T) {
	want := []Inline{{Kind: InlineText, Text: "𠀀___(x__"}}
	if got := ParseInline("𠀀___(x__"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEmphasisDoesNotNarrowUnicodePredecessorToMarker(t *testing.T) {
	want := []Inline{
		{Kind: InlineText, Text: "个"},
		{Kind: InlineEmphasis, Text: "x", Children: []Inline{{Kind: InlineText, Text: "x"}}},
		{Kind: InlineText, Text: "y*"},
	}
	if got := ParseInline("个*x*y*"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// Marked excludes * and _ from the previous-punctuation guard, so a failed
// asterisk cannot admit a punctuation-led opener inside its run.
func TestEmphasisSkipsMixedMarkerSpan(t *testing.T) {
	want := []Inline{{Kind: InlineText, Text: "**__(___*"}}
	if got := ParseInline("**__(___*"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
