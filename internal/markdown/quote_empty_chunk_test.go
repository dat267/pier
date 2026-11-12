package markdown

import "testing"

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
