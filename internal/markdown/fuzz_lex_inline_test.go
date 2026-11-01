package markdown

import "testing"

// FuzzLexAndInline feeds provider-controlled Markdown through the block lexer,
// the inline parser and the incremental re-lex path. A panic here is a
// user-facing crash (model output is untrusted input).
func FuzzLexAndInline(f *testing.F) {
	seeds := []string{
		"", "# h", "- a\n- b", "> q\n> r", "```go\nx\n```", "| a | b |\n|---|---|\n| 1 | 2 |",
		"$$x$$ and $y$", "[l](u(v))", "<a href=\"x\">t</a>", "![i](p)", "***", "~~~~",
		"1. a\n2. b", "\tcode", "a  \nb", "\\[x", "$", "`x", "<!-- c -->", "text\u2028more",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		blocks := Lex(src)
		ParseInlines(blocks)
		document := NewDocument()
		_ = document.Update(src)
		if from := FirstUnstable(blocks, src); from >= 0 && from <= len(src) {
			ParseInlines(LexFrom(src, from))
		}
	})
}
