package markdown

import "testing"

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
