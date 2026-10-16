package markdown

import (
	"strings"
	"testing"
)

func kinds(blocks []*Block) []BlockKind {
	out := make([]BlockKind, len(blocks))
	for i, b := range blocks {
		out[i] = b.Kind
	}
	return out
}

func firstBlock(t *testing.T, src string, kind BlockKind) *Block {
	t.Helper()
	blocks := Lex(src)
	if len(blocks) == 0 {
		t.Fatalf("Lex(%q) produced no blocks", src)
	}
	if blocks[0].Kind != kind {
		t.Fatalf("Lex(%q)[0] kind = %d, want %d (all %v)", src, blocks[0].Kind, kind, kinds(blocks))
	}
	return blocks[0]
}

func TestLexBlockKinds(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		kinds []BlockKind
	}{
		{"paragraph", "one line\n", []BlockKind{BlockParagraph}},
		{"atx heading", "## Title\n", []BlockKind{BlockHeading}},
		{"paragraph then heading", "text\n\n# h\n", []BlockKind{BlockParagraph, BlockHeading}},
		{"thematic break", "***\n", []BlockKind{BlockThematicBreak}},
		{"dash rule", "---\n", []BlockKind{BlockThematicBreak}},
		{"fenced code", "```go\nx\n```\n", []BlockKind{BlockCode}},
		{"blockquote", "> quoted\n", []BlockKind{BlockQuote}},
		{"unordered list", "- a\n- b\n", []BlockKind{BlockList}},
		{"ordered list", "1. a\n2. b\n", []BlockKind{BlockList}},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |\n", []BlockKind{BlockTable}},
		{"html block", "<div>x</div>\n", []BlockKind{BlockHTML}},
		{"indented code", "    code\n", []BlockKind{BlockCode}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocks := Lex(tc.src)
			got := kinds(blocks)
			if len(got) != len(tc.kinds) {
				t.Fatalf("kinds = %v, want %v", got, tc.kinds)
			}
			for i := range got {
				if got[i] != tc.kinds[i] {
					t.Fatalf("kinds = %v, want %v", got, tc.kinds)
				}
			}
		})
	}
}

func TestLexHeadingText(t *testing.T) {
	h := firstBlock(t, "### Some **bold** title ###\n", BlockHeading)
	if h.Level != 3 || h.Text != "Some **bold** title" {
		t.Fatalf("heading = level %d text %q", h.Level, h.Text)
	}
	s := firstBlock(t, "Setext\n======\n", BlockHeading)
	if s.Level != 1 || s.Text != "Setext" {
		t.Fatalf("setext = level %d text %q", s.Level, s.Text)
	}
}

func TestLexParagraphJoinsLines(t *testing.T) {
	b := firstBlock(t, "one\ntwo\nthree\n", BlockParagraph)
	if b.Text != "one\ntwo\nthree" {
		t.Fatalf("paragraph = %q", b.Text)
	}
}

func TestLexParagraphStopsAtBlank(t *testing.T) {
	blocks := Lex("one\ntwo\n\nthree\n")
	if len(blocks) != 2 || blocks[0].Text != "one\ntwo" || blocks[1].Text != "three" {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestLexFenceBodyAndLanguage(t *testing.T) {
	b := firstBlock(t, "```go\nline one\nline two\n```\n", BlockCode)
	if b.Info != "go" {
		t.Fatalf("info = %q", b.Info)
	}
	if b.Body != "line one\nline two\n" {
		t.Fatalf("body = %q", b.Body)
	}
}

func TestLexUnclosedFenceRunsToEnd(t *testing.T) {
	b := firstBlock(t, "```\nstill code\n", BlockCode)
	if b.Body != "still code\n" {
		t.Fatalf("body = %q", b.Body)
	}
}

func TestLexThematicBreakNotHeading(t *testing.T) {
	if k := firstBlock(t, "---\n", BlockThematicBreak).Kind; k != BlockThematicBreak {
		t.Fatalf("kind = %d", k)
	}
}

func TestLexBlockquoteNests(t *testing.T) {
	b := firstBlock(t, "> a\n> b\n", BlockQuote)
	if len(b.Blocks) != 1 || b.Blocks[0].Kind != BlockParagraph || b.Blocks[0].Text != "a\nb" {
		t.Fatalf("quote children = %#v", b.Blocks)
	}
}

func TestLexListItems(t *testing.T) {
	b := firstBlock(t, "- a\n- b\n", BlockList)
	if b.Ordered || len(b.Blocks) != 2 {
		t.Fatalf("list = ordered %v items %d", b.Ordered, len(b.Blocks))
	}
	o := firstBlock(t, "3. a\n4. b\n", BlockList)
	if !o.Ordered || o.StartNumber != 3 {
		t.Fatalf("ordered = %v start %d", o.Ordered, o.StartNumber)
	}
}

func TestLexListItemNesting(t *testing.T) {
	b := firstBlock(t, "- a\n  - a1\n  - a2\n- b\n", BlockList)
	if len(b.Blocks) != 2 {
		t.Fatalf("items = %d", len(b.Blocks))
	}
	nested := b.Blocks[0].Blocks
	if len(nested) != 2 || nested[1].Kind != BlockList {
		t.Fatalf("first item blocks = %v", kinds(nested))
	}
}

func TestLexTable(t *testing.T) {
	b := firstBlock(t, "| a | b |\n|:--|--:|\n| 1 | 2 |\n", BlockTable)
	if len(b.Header) != 2 || b.Header[0] != "a" || b.Header[1] != "b" {
		t.Fatalf("header = %v", b.Header)
	}
	if b.Align[0] != AlignLeft || b.Align[1] != AlignRight {
		t.Fatalf("align = %v", b.Align)
	}
	if len(b.Rows) != 1 || b.Rows[0][1] != "2" {
		t.Fatalf("rows = %v", b.Rows)
	}
}

func TestParseInlineElements(t *testing.T) {
	inlines := ParseInline("plain **bold** *em* ~~del~~ `code` [text](https://x \"t\")")
	kinds := make([]InlineKind, len(inlines))
	for i, in := range inlines {
		kinds[i] = in.Kind
	}
	want := []InlineKind{
		InlineText, InlineStrong, InlineText, InlineEmphasis, InlineText,
		InlineStrikethrough, InlineText, InlineCode, InlineText, InlineLink,
	}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	link := inlines[len(inlines)-1]
	if link.Dest != "https://x" || link.Title != "t" {
		t.Fatalf("link = %#v", link)
	}
}

func TestParseInlineCodeSpan(t *testing.T) {
	inlines := ParseInline("a `x  y` b")
	if len(inlines) != 3 || inlines[1].Kind != InlineCode || inlines[1].Text != "x  y" {
		t.Fatalf("inlines = %#v", inlines)
	}
}

func TestParseInlineEscape(t *testing.T) {
	inlines := ParseInline(`a \*b\* c`)
	if len(inlines) != 1 || inlines[0].Text != "a *b* c" {
		t.Fatalf("inlines = %#v", inlines)
	}
}

func TestParseInlineImage(t *testing.T) {
	inlines := ParseInline("![alt](img.png)")
	if len(inlines) != 1 || inlines[0].Kind != InlineImage || inlines[0].Dest != "img.png" {
		t.Fatalf("inlines = %#v", inlines)
	}
}

func TestParseInlineAutolink(t *testing.T) {
	inlines := ParseInline("<https://example.com>")
	if len(inlines) != 1 || inlines[0].Kind != InlineLink || inlines[0].Dest != "https://example.com" {
		t.Fatalf("inlines = %#v", inlines)
	}
}

func TestParseInlineHardBreak(t *testing.T) {
	inlines := ParseInline("a  \nb")
	found := false
	for _, in := range inlines {
		if in.Kind == InlineHardBreak {
			found = true
		}
	}
	if !found {
		t.Fatalf("no hard break in %#v", inlines)
	}
}

// TestLexFixtureStructure checks the benchmark document parses into the
// expected repeating shape.
func TestLexFixtureStructure(t *testing.T) {
	blocks := Lex(benchDoc(4096))
	for _, b := range blocks {
		switch b.Kind {
		case BlockHeading, BlockParagraph, BlockCode, BlockList:
		default:
			t.Fatalf("unexpected block kind %d", b.Kind)
		}
	}
	if len(blocks) < 8 {
		t.Fatalf("blocks = %d", len(blocks))
	}
}

// TestLexDoesNotSwallowDocument is the regression for the list content-indent
// bug: a list item must not consume the blocks after the list.
func TestLexDoesNotSwallowDocument(t *testing.T) {
	src := "- one\n- two\n\n```go\ncode\n```\n\ntail\n"
	blocks := Lex(src)
	if got := kinds(blocks); len(got) != 3 || got[0] != BlockList || got[1] != BlockCode || got[2] != BlockParagraph {
		t.Fatalf("kinds = %v", got)
	}
	if blocks[0].End > strings.Index(src, "```") {
		t.Fatalf("list end %d overruns the fence at %d", blocks[0].End, strings.Index(src, "```"))
	}
}
