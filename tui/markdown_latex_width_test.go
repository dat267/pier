package tui

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), components/markdown.ts:586-591 and the
// upstream assertions in test/markdown.test.ts for issues 11483 and 11159,
// ported verbatim. A rendered display-math layout cannot be wrapped row by row,
// so upstream falls back to the raw `$$`/`\[` source when any rendered line is
// wider than the content width.
func TestMarkdownLatexWidthFallback(t *testing.T) {
	render := func(source string, width int, options MarkdownOptions) []string {
		identity := func(text string) string { return text }
		theme := MarkdownTheme{
			Heading: identity, Link: identity, LinkURL: identity, Code: identity,
			CodeBlock: identity, CodeBlockBorder: identity, Quote: identity,
			QuoteBorder: identity, Hr: identity, ListBullet: identity,
			Bold: identity, Italic: identity, Strikethrough: identity, Underline: identity,
		}
		lines := NewMarkdown(source, 0, 0, theme, nil, options).Render(width)
		for i, line := range lines {
			lines[i] = strings.TrimRight(line, " ")
		}
		return lines
	}

	t.Run("raw-math-preserved-when-rendering-is-off", func(t *testing.T) {
		noLatex := false
		source := "$$\n\\widetilde Y_{sf}\n= \n(1-w_{sf})\\mu_{sf}^{\\mathrm{MAR}}\n$$\n\nInline \\(A_{sf}\\)"
		assertMarkdownLines(t, render(source, 80, MarkdownOptions{RenderLatex: &noLatex}), []string{
			"$$", `\widetilde Y_{sf}`, "=", `(1-w_{sf})\mu_{sf}^{\mathrm{MAR}}`, "$$", "", `Inline \(A_{sf}\)`,
		})
	})

	t.Run("rendered-when-it-fits", func(t *testing.T) {
		source := "\\[\n\\frac{\\partial^2 u}{\\partial x^2} + \\frac{\\partial^2 u}{\\partial y^2} = \\sum_{i=1}^{n} \\frac{1}{i^2} + \\int_0^1 f(x)\\,dx\n\\]"
		assertMarkdownLines(t, render(source, 40, MarkdownOptions{}), []string{
			"∂² u   ∂² u   n   1   1",
			"──── + ──── = ∑   ── +∫ f(x) dx",
			"∂ x²   ∂ y²  i=1  i²  0",
		})
	})

	t.Run("falls-back-to-raw-source-when-wider-than-content", func(t *testing.T) {
		source := "\\[\n\\frac{\\partial^2 u}{\\partial x^2} + \\frac{\\partial^2 u}{\\partial y^2} = \\sum_{i=1}^{n} \\frac{1}{i^2} + \\int_0^1 f(x)\\,dx\n\\]"
		narrow := render(source, 30, MarkdownOptions{})
		if narrow[0] != "\\[" || narrow[len(narrow)-1] != "\\]" {
			t.Fatalf("narrow render = %q, want the raw \\[ … \\] source", narrow)
		}
		for _, line := range narrow {
			if strings.Contains(line, "─") {
				t.Errorf("narrow render kept a rendered formula line: %q", line)
			}
			if width := VisibleWidth(line); width > 30 {
				t.Errorf("narrow line is %d wide, content width is 30: %q", width, line)
			}
		}
	})

	table := []string{"| A | B |", "|---|---|", "| 1 | 2 |"}
	renderedTable := []string{"┌───┬───┐", "│ A │ B │", "├───┼───┤", "│ 1 │ 2 │", "└───┴───┘"}

	t.Run("table-after-orphan-display-opener", func(t *testing.T) {
		source := strings.Join(append(append([]string{"Intro", "", "$$", ""}, table...), "", "After"), "\n")
		want := append(append([]string{"Intro", "", "$$", ""}, renderedTable...), "", "After")
		assertMarkdownLines(t, render(source, 80, MarkdownOptions{}), want)
	})

	t.Run("table-after-orphan-opener-with-math-like-content", func(t *testing.T) {
		source := strings.Join(append([]string{"$$", "x = 1", ""}, table...), "\n")
		assertMarkdownLines(t, render(source, 80, MarkdownOptions{}), append([]string{"$$", "x = 1", ""}, renderedTable...))
	})

	t.Run("unclosed-opener-does-not-pair-across-a-table", func(t *testing.T) {
		source := strings.Join(append(append([]string{"Intro", "", "$$", "x = 1", ""}, table...), "", "$$", "y^2", "$$"), "\n")
		want := append(append([]string{"Intro", "", "$$", "x = 1", ""}, renderedTable...), "", "y²")
		assertMarkdownLines(t, render(source, 80, MarkdownOptions{}), want)
	})

	t.Run("single-column-table-with-pipe-free-delimiter", func(t *testing.T) {
		source := strings.Join([]string{"$$", "", "| A |", ":---:", "| 1 |"}, "\n")
		assertMarkdownLines(t, render(source, 80, MarkdownOptions{}), []string{"$$", "", "┌───┐", "│ A │", "├───┤", "│ 1 │", "└───┘"})
	})
}

// assertMarkdownLines compares rendered lines exactly, naming the first
// difference so a layout change is legible.
func assertMarkdownLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("lines = %d, want %d\ngot  %q\nwant %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q\nall got %q", i, got[i], want[i], got)
		}
	}
}
