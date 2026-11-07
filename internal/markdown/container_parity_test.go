package markdown

import "testing"

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11, which upstream
// packages/tui/src/components/markdown.ts lexes with. list.go ports
// Tokenizer.list and quote.go ports Tokenizer.blockquote's chunk loop; `want`
// strings are marked's tokens projected by referenceShape.
func TestContainerTokenizersMatchMarked(t *testing.T) {
	for _, testCase := range []struct{ src, want string }{
		// A410: the chunk loop keeps lexing the lines after a lazy chunk inside
		// the quote, and stops only after a code block.
		{"> a\n| x |\n| - |\n", `quote[p:"a",table:1x0]`},
		{"> a\n<br>\n", `quote[p:"a",html]`},
		{"> a\n> ```\nb\n", `quote[p:"a",code:""] | p:"b"`},
		// A nested quote or a list as the last token re-lexes with the rest.
		{"> > a\nb\n> c\n", `quote[quote[p:"a\nb\nc"]]`},
		{"> - a\nb\n> - c\n", `quote[ul[(p:"a\nb")],ul[(p:"c")]]`},

		// A412: blank lines stay inside an item, so two of them between items
		// keep one list (the first item ends in a space token).
		{"- a\n\n\n- b\n", `ul[(p:"a",space),(p:"b")]`},
		{"- a\n\n- b\n", `ul[(p:"a"),(p:"b")]`},
		{"- a\n\n  b\n- c\n", `ul[(p:"a",space,p:"b"),(p:"c")]`},

		// itemRegex(bull) continues a list only with the same bullet or
		// ordered delimiter.
		{"- a\n+ b\n", `ul[(p:"a")] | ul[(p:"b")]`},
		{"1. a\n2) b\n", `ol[(p:"a")] | ol[(p:"b")]`},

		// An item's indented-code line is a lazy paragraph line; an empty first
		// line followed by a blank line ends the item.
		{"- a\n    code\n", `ul[(p:"a\n  code")]`},
		{"-\n\n  a\n", `ul[()] | space | p:"  a"`},

		// The heading rule takes at most three spaces of indent.
		{"    # not a heading\n", `code:"# not a heading\n"`},
		{"- a\n# h\n", `ul[(p:"a")] | h1:"h"`},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want {
			t.Errorf("%q:\n got %s\nwant %s", testCase.src, got, testCase.want)
		}
	}
}
