package markdown

import (
	"fmt"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11, which upstream
// packages/tui/src/components/markdown.ts lexes with. A container absorbs a
// following plain line into its last paragraph ("lazy continuation"): the
// blockquote rule is /^( {0,3}> ?(paragraph|[^\n]*)(?:\n|$))+/, whose
// `paragraph` is the multi-line paragraph pattern, so a line without the `>`
// marker still belongs to the quote; the list tokenizer appends such a line to
// the item's text (`c += "\n" + d`).
//
//   - a        ul[(p:"a\nb")]        > a        quote[p:"a\nb"]
//     b                                 b
//
// The continuation stops at a line that starts another block, and does not
// apply when the container's last child is not a paragraph. `want` strings are
// marked's projected output.
func referenceShape(blocks []*Block) string {
	return referenceShapeJoin(blocks, " | ")
}

// referenceShapeJoin projects blocks the way the reference's tokens nest:
// top-level blocks are joined with " | ", a container's children with ",".
func referenceShapeJoin(blocks []*Block, sep string) string {
	out := ""
	for _, b := range blocks {
		if out != "" {
			out += sep
		}
		switch b.Kind {
		case BlockSpace:
			out += "space"
		case BlockCode:
			out += "code:" + fmt.Sprintf("%q", b.Body)
		case BlockHeading:
			out += fmt.Sprintf("h%d:%q", b.Level, b.Text)
		case BlockParagraph:
			out += "p:" + fmt.Sprintf("%q", b.Text)
		case BlockQuote:
			out += "quote[" + referenceShapeJoin(b.Blocks, ",") + "]"
		case BlockList:
			kind := "ul"
			if b.Ordered {
				kind = "ol"
			}
			out += kind + "["
			for i, item := range b.Blocks {
				if i > 0 {
					out += ","
				}
				out += "(" + referenceShapeJoin(item.Blocks, ",") + ")"
			}
			out += "]"
		case BlockTable:
			out += fmt.Sprintf("table:%dx%d", len(b.Header), len(b.Rows))
		case BlockThematicBreak:
			out += "hr"
		case BlockHTML:
			out += "html"
		case BlockLinkDef:
			out += "def:" + fmt.Sprintf("%q", b.Text)
		default:
			out += fmt.Sprint(b.Kind)
		}
	}
	return out
}

func TestLazyContinuationJoinsTheLastParagraph(t *testing.T) {
	for _, testCase := range []struct{ src, want string }{
		// The reported failure: a prose line after the last item.
		{"- a\nb\n", `ul[(p:"a\nb")]`},
		{"- a\nb\nc\n", `ul[(p:"a\nb\nc")]`},
		{"1. a\n2. b\nc\n", `ol[(p:"a"),(p:"b\nc")]`},
		{"- a\n  - b\nlazy\n", `ul[(p:"a",ul[(p:"b\nlazy")])]`},

		// A blockquote absorbs the line without its `>` marker.
		{"> a\nb\n", `quote[p:"a\nb"]`},
		{"> a\nb\nc\n", `quote[p:"a\nb\nc"]`},
		{"> a\nb\n> c\n", `quote[p:"a\nb\nc"]`},
		{"> - a\n> - b\nlazy\n", `quote[ul[(p:"a"),(p:"b\nlazy")]]`},

		// A block start still interrupts: heading, rule, quote, other list.
		{"- a\n# h\ntail\n", `ul[(p:"a")] | h1:"h" | p:"tail"`},
		{"- a\nb\n# h\nc\n", `ul[(p:"a\nb")] | h1:"h" | p:"c"`},
		{"- a\n2. b\n", `ul[(p:"a")] | ol[(p:"b")]`},
		{"- a\nlazy\n> q\n", `ul[(p:"a\nlazy")] | quote[p:"q"]`},
		{"> a\n# h\n", `quote[p:"a"] | h1:"h"`},
		{"> a\n- b\n", `quote[p:"a"] | ul[(p:"b")]`},
		{"> a\n```\nx\n```\n", `quote[p:"a"] | code:"x"`},

		// A setext underline without the `>` marker does not make a heading.
		// Alone in its chunk, the underline has no preceding newline for
		// blockquoteSetextReplace to indent, so it joins the paragraph as is.
		{"> a\n===\n", `quote[p:"a\n==="]`},
		{"> a\nb\n===\n", `quote[p:"a\nb\n    ==="]`},

		// No continuation when the container's last child is not a paragraph.
		{"- # h\nlazy\n", `ul[(h1:"h")] | p:"lazy"`},
		{"- ```\n  x\n  ```\nlazy\n", `ul[(code:"x")] | p:"lazy"`},

		// A blank line ends the container.
		{"- a\n\nlazy\n", `ul[(p:"a")] | space | p:"lazy"`},
		{"> a\n\nb\n", `quote[p:"a"] | space | p:"b"`},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want {
			t.Errorf("%q:\n got %s\nwant %s", testCase.src, got, testCase.want)
		}
	}
}
