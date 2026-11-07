package markdown

import "testing"

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11, which upstream
// packages/tui/src/components/markdown.ts lexes with. Its paragraph rule has
// two html alternatives: the block-start one accepts any type-6 tag while the
// continuation (inside the `paragraph` alternation repeated per line) is
// `</?(?:tag)(?: +|\n|/?>)|<(?:script|pre|style|textarea|!--)`, so a tag that
// neither opens a type-1..6 block nor is one of the named ones does NOT
// interrupt a paragraph. A7 HTML (`<br>`, `<span>`) continues the paragraph's
// text.
func TestTypeSevenHTMLDoesNotInterruptParagraph(t *testing.T) {
	for _, testCase := range []struct {
		src  string
		want string
	}{
		{"a\n<br>\nb\n", `p:"a\n<br>\nb"`},
		{"a\n<span>x</span>\nb\n", `p:"a\n<span>x</span>\nb"`},
		// The full reference behavior of the completing bytes:
		{"a\n<div>\nb\n", `p:"a" | html`},
		{"a\n<!-- c -->\nb\n", `p:"a" | html | p:"b"`},
		// Control: after a blank line the paragraph is over, so the same tag
		// starts a block.
		{"a\n\n<br>\n", `p:"a" | space | html`},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want {
			t.Errorf("%q:\n got %s\nwant %s", testCase.src, got, testCase.want)
		}
	}
}
