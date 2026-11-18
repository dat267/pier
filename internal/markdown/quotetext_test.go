package markdown

import (
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) marked 18.0.11 Tokenizer.blockquote, which
// upstream packages/tui/src/components/markdown.ts lexes with. When a chunk
// ends in a list, the tokenizer re-lexes the list with the following lazy lines
// and replaces the list's text with the re-lexed raw, then splits the leftover
// (`"".split("\n")` is one empty line, whose chunk adds a separator). When it
// ends in a nested quote, the nested quote's text is replaced by the re-lexed
// one. Pier kept the first chunk's text and dropped the continuation.
func TestBlockquoteTextKeepsLazyContinuations(t *testing.T) {
	for _, testCase := range []struct {
		src  string
		text string
	}{
		{"> - a\nb\n", "- a\nb\n"},
		{"> - a\nb\nc\n", "- a\nb\nc\n"},
		{"> > > a\n> b\n", "> > a\nb"},
		{"> > - a\nb\n", "> - a\nb\n"},
		{"> > a\n> b\nc\n", "> a\nb\nc"},
		{"> a\nb\n", "a\nb"},
	} {
		blocks := Lex(testCase.src)
		if len(blocks) == 0 || blocks[0].Kind != BlockQuote {
			t.Fatalf("%q: blocks = %v, want a quote", testCase.src, blockKinds(blocks))
		}
		if blocks[0].Text != testCase.text {
			t.Errorf("%q: text = %q, want %q", testCase.src, blocks[0].Text, testCase.text)
		}
	}
}

// pi v1.5.2 (b4afdddf): marked's blockquote tokenizer strips the marker from
// every line, so a marker line with no content after it contributes neither
// text nor a separator newline to the quote's token text. Lexing such a quote
// panicked with an index out of range (A531) when an unmarked line followed.
func TestQuoteContentlessChunkContributesNothing(t *testing.T) {
	cases := []struct {
		src   string
		text  string
		inner string
	}{
		// The empty first line contributes nothing to the token text.
		{">\t\n0", "0", "0"},
		{">\t\n0\n1", "0\n1", "0\n1"},
		// A separately marked line is its own chunk, so its separator stays.
		{">\t\n> 0", "\n0", "0"},
		// A lazy continuation after content is unchanged.
		{"> a\n0", "a\n0", "a\n0"},
	}
	for _, testCase := range cases {
		blocks := Lex(testCase.src)
		if len(blocks) != 1 || blocks[0].Kind != BlockQuote {
			t.Fatalf("Lex(%q) = %+v, want one blockquote", testCase.src, blocks)
		}
		if blocks[0].Text != testCase.text {
			t.Errorf("Lex(%q) quote text = %q, want %q", testCase.src, blocks[0].Text, testCase.text)
		}
		var paragraph string
		for _, child := range blocks[0].Blocks {
			if child.Kind == BlockParagraph {
				paragraph += child.Text
			}
		}
		if paragraph != testCase.inner {
			t.Errorf("Lex(%q) paragraph = %q, want %q", testCase.src, paragraph, testCase.inner)
		}
	}
}
