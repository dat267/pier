package markdown

// The layout layer renders arbitrary Markdown at any width: no panic for
// unterminated fences, tables, math or control characters.

import "testing"

func FuzzRenderSource(f *testing.F) {
	for _, seed := range []string{"", "# h\n\npara", "```go\nx\n```", "- a\n- b", "| a | b |\n|---|---|\n| 1 | 2 |", "$x$", "\x1b[31m"} {
		f.Add(seed, 0)
		f.Add(seed, 40)
	}
	f.Fuzz(func(t *testing.T, src string, width int) {
		if width < 0 {
			width = -width
		}
		width %= 200
		_ = RenderSource(src, width, NopStyle{})
	})
}
