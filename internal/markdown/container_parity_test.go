package markdown

import (
	"testing"
)

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

		// pi v1.6.0 (faac5858) marked 18.0.11 Tokenizer.list tests blank lines
		// with JS `trim()`, slices continuation lines by UTF-16 units and trims
		// the list with `trimEnd()`: U+FEFF and U+3000 are space and U+0085 is
		// text, and a blank line of either space is skipped whole, not cut
		// mid-character.
		{"- \uFEFF\n\n  a\n", `ul[()] | p:" \ufeff" | space | p:"  a"`},
		{"- a\n\uFEFF\n  b\n- c\n", `ul[(p:"a",space,p:"b"),(p:"c")]`},
		{"- a\n\u3000\n  b\n- c\n", `ul[(p:"a",space,p:"b"),(p:"c")]`},
		{"- a\n\u0085\n  b\n", `ul[(p:"a\n\u0085\nb")]`},
		{"- a \uFEFF\n", `ul[(p:"a")] | p:" \ufeff"`},

		// Tokenizer.list trims the list's end and blockTokens folds the one-space
		// token after it into the raw as "\n", so the quote re-lexes `- a\n\nb`:
		// a lazy line after an item ending in a space is its own paragraph
		// (pi v1.6.0, faac5858).
		{"> - a \nb", `quote[ul[(p:"a")],space,p:"b"]`},
		{"> 1. a \nb\nc", `quote[ol[(p:"a")],space,p:"b\nc"]`},
		{"> - a\nb", `quote[ul[(p:"a\nb")]]`},

		// The heading rule takes at most three spaces of indent.
		{"    # not a heading\n", `code:"# not a heading\n"`},
		{"- a\n# h\n", `ul[(p:"a")] | h1:"h"`},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want {
			t.Errorf("%q:\n got %s\nwant %s", testCase.src, got, testCase.want)
		}
	}
}

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11, which upstream
// packages/tui/src/components/markdown.ts lexes with. marked's code tokenizer
// runs the raw through `te`: trailing lines that are blank or whitespace-only
// are cut from the raw when two or more of them follow the last content line
// (the piece after a final newline counts), and the cut lines then start a
// space token at top level. A single trailing newline stays in the text
// (`    indented\n` keeps `text: "indented\n"`), and the text never ends in a
// second newline.
func TestIndentedCodeTrailingBlankLines(t *testing.T) {
	for _, testCase := range []struct {
		src   string
		want  string
		after string
	}{
		// A whitespace-only trailing line plus the final newline counts as two
		// blankish pieces: both leave the block.
		{"    code\n \n", `code:"code"`, ` | space`},
		{"\t- t\n \n", `code:"- t"`, ` | space`},
		// Empty trailing lines (more than one piece) leave the block too.
		{"    a\n\n\n", `code:"a"`, ` | space`},
		{"    a\n\t\n", `code:"a"`, ` | space`},
		// A single trailing newline stays in the text.
		{"    indented\n", `code:"indented\n"`, ``},
		// One blankish line before a following paragraph stays in the text.
		{"    a\n \ntext\n", `code:"a"`, ` | space | p:"text"`},
		// Content lines in the middle keep their newlines.
		{"    a\n    b\n", `code:"a\nb\n"`, ``},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want+testCase.after {
			t.Errorf("%q:\n got %s\nwant %s%s", testCase.src, got, testCase.want, testCase.after)
		}
	}
}
