package tui

import "testing"

// FuzzLexMarkdown checks the lexer never panics on arbitrary input; the seeds
// cover the shapes the renderer consumes.
func FuzzLexMarkdown(f *testing.F) {
	seeds := []string{
		"# heading\n\nparagraph with **bold** and `code`\n",
		"- [ ] task\n- item\n\n> quote\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n",
		"```go\nfmt.Println(\"x\")\n```\n",
		"$$\\frac{a}{b}$$\n",
		"<div>html</div>\n\n---\n",
		"unclosed `code and **bold\n\n```\n",
		"\x00\xff\xfe",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		_ = LexMarkdown(source)
	})
}
