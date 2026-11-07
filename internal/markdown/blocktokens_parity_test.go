package markdown

import (
	"slices"
	"testing"
	"time"
)

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11, which upstream
// packages/tui/src/components/markdown.ts lexes with (Lexer.blockTokens, the
// block tokenizers and rules.ts). `want` strings are marked's tokens projected
// by referenceShape.
func TestBlockTokensMatchMarked(t *testing.T) {
	for _, testCase := range []struct{ src, want string }{
		// Below the top level blockTokens lexes text line by line: a line that starts
		// a block ends the text, and indented code, a definition or a later text line
		// joins it (Tokenizer.text, marked 18.0.11). lheading is tried again at each
		// text line.
		{"- a\n  b\n      c\n", `ul[(p:"a\nb\nc")]`},
		{"- a\nb\n      c\n", `ul[(p:"a\nb\nc")]`},
		{"- a\n      c\n", `ul[(p:"a\nc")]`},
		{"- a\n    code\n  b\n", `ul[(p:"a\n  code\nb")]`},
		{"- a\n  [x]: /u\n", `ul[(p:"a\n[x]: /u")]`},
		{"- a\n  - b\n      c\n", `ul[(p:"a",ul[(p:"b\n  c")])]`},
		{"- a\n  [x]: /u\n  [y]: /v\n", `ul[(p:"a\n[x]: /u\n[y]: /v")]`},
		{"- b\n  [x]: /u\n  | x |\n", `ul[(p:"b\n[x]: /u\n| x |")]`},
		{"- | - |\n  y\n  ===\n", `ul[(p:"| - |",h1:"y")]`},
		{"- x\n  | - |\n  y\n  ===\n", `ul[(table:1x2)]`},

		// Tokenizer.def trims its trailing newlines, so blank lines after a definition
		// are a space token; blockTokens joins a definition after a paragraph or text
		// token as text.
		{"[x]: /u\n\n- b\n", `def:"x" | space | ul[(p:"b")]`},
		{"a\n[x]: /u\n\nb\n", `p:"a\n[x]: /u" | space | p:"b"`},
		{"> q\n[x]: /u", `quote[p:"q\n[x]: /u"]`},
		{"> q\n> [x]: /u\n", `quote[p:"q\n[x]: /u"]`},
		{"* a\n  [x]: /u\n\n  - t\n", `ul[(p:"a\n[x]: /u",space,ul[(p:"t")])]`},

		// blockTokens joins indented code after a paragraph at any level.
		{"a\n    b\n---\n", `p:"a\nb\n" | hr`},
		{"a\n    b\n-:\n", `p:"a\nb\n" | p:"-:"`},

		// The html rule's start conditions 1-7: type 7 needs one complete open or
		// closing tag alone on its line, so an inline element that starts a line is a
		// paragraph.
		{"<span>x</span>\n", `p:"<span>x</span>"`},
		{"<span>\nx\n", `html`},
		{"<span a=\"1\">\nx\n", `html`},
		{"</span> x\n", `p:"</span> x"`},
		{"<div>x</div>\n", `html`},
		{"<a href=\"u>\nx\n", `p:"<a href=\"u>\nx"`},
		{"<kbd>Ctrl</kbd>+**C**\n", `p:"<kbd>Ctrl</kbd>+**C**"`},
		{"<!-- c\n-->\nx", `html | p:"x"`},
		{"<td>a\n", `html`},
		{"<p/>x\n", `html`},
		{"<x-y_z a>\n", `html`},

		// The html rule ends types 6 and 7 at a blank line followed by a newline; a
		// whitespace-only last line stays in the block.
		{"<br>\n> > n\n ", `html`},
		{"<br>\na\n \n", `html | space`},
		{"<div>\na\n  \nb", `html | space | p:"b"`},

		// Tokenizer.lheading trims its text.
		{"  b\n---\n10. a\n", `h2:"b" | ol[(p:"a")]`},

		// Tokenizer.table needs a pipe or a colon in the delimiter row and as many
		// header cells as delimiter cells; splitCells keeps an escaped pipe.
		{"| x |\n-\n", `h2:"| x |"`},
		{"| x |\n:-\n", `table:1x0`},
		{"x\n:-\n", `table:1x0`},
		{"x y\n:-: | -\n", `p:"x y\n:-: | -"`},
		{"a \\| b\n-\n", `h2:"a \\| b"`},
		{"a \\| b | c\n- | -\n", `table:2x0`},

		// The paragraph rule stops before a line that has gfmTable's shape with the
		// line after it, even when Tokenizer.table then rejects the rows.
		{"a\n2. b\n---\n", `p:"a" | ol[(p:"b")] | hr`},
		{"a\nb\n-:\n", `p:"a" | table:1x0`},
		{"a\nb c\n-|-\n", `p:"a" | p:"b c\n-|-"`},
		{"| - |\nb\nc\n-\n", `p:"| - |\nb" | h2:"c"`},
		{"a\n<x>\n---\n", `p:"a" | html`},

		// lheading runs before the paragraph rule and its text runs on past lines
		// its lookahead accepts; when it fails, no setext underline interrupts the
		// paragraph.
		{"b c\n10. a\n===", `p:"b c\n10. a\n==="`},
		{"| - |\n- \na\n", `p:"| - |\n- \na"`},
		{"a\nb\n---\n", `h2:"a\nb"`},
		{"a\n<div>x\n---\n", `h2:"a\n<div>x"`},
		{"a\n<div>\n---\n", `p:"a" | html`},
		{"a\n<a href=x>y</a>\n---\n", `h2:"a\n<a href=x>y</a>"`},
		{"a\n***\n---\n", `h2:"a\n***"`},
		{"a\n2. b\n===\n", `p:"a\n2. b\n==="`},
		{"    code\n| - |\n---", `code:"code\n" | p:"| - |" | hr`},
	} {
		if got := referenceShape(Lex(testCase.src)); got != testCase.want {
			t.Errorf("%q:\n got %s\nwant %s", testCase.src, got, testCase.want)
		}
	}
}

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11 Tokenizer.list. listIsTask
// `^\[[ xX]\] +\S` marks an item, but the checkbox is only placed when the
// item's first token is text or a paragraph; otherwise item.task is reset and
// `[ ]` stays in the token's text.
func TestTaskCheckboxNeedsAFirstTextToken(t *testing.T) {
	for _, testCase := range []struct {
		src           string
		task, checked bool
		first         string
	}{
		{"- [ ] t\n===\n", false, false, `h1:"[ ] t"`},
		{"- [ ] t\n  | - |\n", false, false, `table:1x0`},
		{"- [ ] t\n", true, false, `p:"t"`},
		{"- [x]  t\n", true, true, `p:"t"`},
		{"- [ ]\tt\n", true, false, `p:"t"`},
		{"- [X]\n", false, false, `p:"[X]"`},
		{"- [ ] # h\n", true, false, `p:"# h"`},
	} {
		item := Lex(testCase.src)[0].Blocks[0]
		first := referenceShape(item.Blocks[:1])
		if item.Task != testCase.task || item.Checked != testCase.checked || first != testCase.first {
			t.Errorf("%q: task=%v checked=%v first=%s, want task=%v checked=%v first=%s",
				testCase.src, item.Task, item.Checked, first, testCase.task, testCase.checked, testCase.first)
		}
	}
}

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11 def rule, title alternative
// `"(?:\\"?|[^"\\])*"|'[^'\n]*(?:\n[^'\n]+)*\n?'|\([^()]*\)`, and
// Tokenizer.def (the title loses its delimiters and backslash escapes). A
// single-quoted title used to loop forever.
func TestLinkDefinitionTitleForms(t *testing.T) {
	for _, testCase := range []struct{ src, shape, title string }{
		{"[a]: /u 'q'\n", `def:"a"`, "q"},
		{"[a]: /u 'q\nr'\n", `def:"a"`, "q\nr"},
		{"[a]: /u 'q\n'\n", `def:"a"`, "q\n"},
		{"[a]: /u ''\n", `def:"a"`, ""},
		{"[a]: /u 'q\n\nr'\n", `p:"[a]: /u 'q" | space | p:"r'"`, ""},
		{"[a]: /u (p)\n", `def:"a"`, "p"},
		{"[a]: /u (p(q))\n", `p:"[a]: /u (p(q))"`, ""},
		{"[a]: /u \"a\\\"\n", `def:"a"`, `a\`},
		{"[a]: /u \"a\\\"b\"\n", `def:"a"`, `a"b`},
	} {
		done := make(chan []*Block, 1)
		go func() { done <- Lex(testCase.src) }()
		var blocks []*Block
		select {
		case blocks = <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%q: Lex did not return", testCase.src)
		}
		if got := referenceShape(blocks); got != testCase.shape {
			t.Errorf("%q:\n got %s\nwant %s", testCase.src, got, testCase.shape)
			continue
		}
		_, defs := LexDefinitions(testCase.src, 0, nil)
		if blocks[0].Kind == BlockLinkDef && (len(defs) != 1 || defs[0].Title != testCase.title) {
			t.Errorf("%q: definitions %+v, want title %q", testCase.src, defs, testCase.title)
		}
	}
}

// Reference: marked 18.0.11 Lexer.blockTokens keeps the first definition of a
// tag (`!this.tokens.links[token.tag]`) at any depth and pushes no token for a
// later one; Tokenizer.def lowercases the tag and collapses its whitespace.
// LexDefinitions carries the definitions before a re-lex offset.
func TestLexDefinitionsKeepsTheFirstOfEachTag(t *testing.T) {
	src := "[X  Y]: /a\n\n- [x y]: /b\n  [z]: <c> \"t\"\n\n> [Z]: /d\n"
	blocks, defs := LexDefinitions(src, 0, nil)
	// referenceShape prints a definition's label as written; marked's tag is
	// the normalized "x y".
	if got, want := referenceShape(blocks), `def:"X  Y" | space | ul[(space,def:"z")] | space | quote[]`; got != want {
		t.Fatalf("shape:\n got %s\nwant %s", got, want)
	}
	want := []Definition{{Offset: 0, Label: "x y", Href: "/a"}, {Offset: 12, Label: "z", Href: "c", Title: "t"}}
	if !slices.Equal(defs, want) {
		t.Fatalf("definitions %+v, want %+v", defs, want)
	}
	// Re-lexing the tail with the definitions before it drops the repeated one.
	tail, tailDefs := LexDefinitions(src+"\n[x  y]: /e\n", 40, DefinitionsBefore(defs, 40))
	if got, want := referenceShape(tail), `quote[] | space`; got != want {
		t.Fatalf("tail shape:\n got %s\nwant %s", got, want)
	}
	if !SameDefinitions(tailDefs, defs) {
		t.Fatalf("tail definitions %+v, want %+v", tailDefs, defs)
	}
}
