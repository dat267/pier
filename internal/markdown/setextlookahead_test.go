package markdown

import "testing"

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11, which upstream
// packages/tui/src/components/markdown.ts lexes with. Its lheading rule is
// ^(?! (any-list|block code|fence|quote|heading|html-open|delimiter-row))
// ((?:.|\n(?! same alternation))+?)\n {0,3}(=+|-+) *(?:\n+|$) — the paragraph
// only becomes a heading after lines marked's lheading continuation accepts:
//
//  2. b        any ordered number, not only 1, blocks the conversion
//     | - |       a delimiter-row-shaped first line blocks the conversion
//
// When the conversion is blocked, the paragraph rule lexes the same lines and
// no underline interrupts it; `x\n2. b\n---` still ends before `2. b` because
// those two lines have gfmTable's shape (blocktokens_parity_test.go).
func TestSetextUnderlineFollowsTheLHeadingLookahead(t *testing.T) {
	for _, testCase := range []struct {
		src  string
		want string
	}{
		// The tracker's first case: the whole pair stays a paragraph.
		{"| - |\n===\n", `p:"| - |\n==="`},
		{"||| - |||\n===\n", `p:"||| - |||\n==="`},
		// The tracker's second case: the paragraph stops before "10. a" (its
		// line and the dashes have gfmTable's shape), which is a list on its
		// own, and the dashes are a rule.
		{"| x |\n10. a\n---\n", `p:"| x |" | ol[(p:"a")] | hr`},
		// A bullet line blocks the same way.
		{"| x |\n- a\n---\n", `p:"| x |" | ul[(p:"a")] | hr`},
		{"a\n2. b\n---\n", `p:"a" | ol[(p:"b")] | hr`},
		// Controls: without a list line the underline still makes a heading.
		{"text\nmore\n---\n", `h2:"text\nmore"`},
		{"| a | b |\n===\n", `h1:"| a | b |"`},
		{"|| x ||\n===\n", `h1:"|| x ||"`},
		{"x\ny\n---\n", `h2:"x\ny"`},
		// An interrupted paragraph cannot take the underline: the list wins.
		{"a\n1. b\n---\n", `p:"a" | ol[(p:"b")] | hr`},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want {
			t.Errorf("%q:\n got %s\nwant %s", testCase.src, got, testCase.want)
		}
	}
}

// Reference: pi v1.5.2 (b4afdddf) marked 18.0.11, whose lheading lookahead's
// delimiter-row alternative is ` {0,3}\|?(?:[:\- ]*\|)+[\:\- ]*(?:\n|$)`: every
// unit ends in a pipe, so a line of colons, dashes and spaces alone does not
// veto the heading. The port used to treat any such tail as delimiter shaped,
// which dropped the heading: `a\n: \n---` stayed three blocks.
func TestSetextHeadingSurvivesAPipeLessDelimiterLikeLine(t *testing.T) {
	for _, testCase := range []struct {
		src  string
		want string
	}{
		{"a\n: \n---\n", `h2:"a\n:"`},
		{"a\n::\n---\n", `h2:"a\n::"`},
		{"a\n - \n---\n", `h2:"a" | hr`},
		// A pipe anywhere in the tail keeps the veto.
		{"a\n| |\n---\n", `p:"a" | p:"| |" | hr`},
		{"a\n:|\n---\n", `p:"a" | p:":|" | hr`},
		{"a\n |\n---\n", `p:"a" | p:" |" | hr`},
		{"a\n-|-\n---\n", `p:"a" | p:"-|-" | hr`},
		// A delimiter-shaped first line still blocks the conversion.
		{"| - |\n===\n", `p:"| - |\n==="`},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want {
			t.Errorf("%q:\n got %s\nwant %s", testCase.src, got, testCase.want)
		}
	}
}
