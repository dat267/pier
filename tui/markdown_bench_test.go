package tui

import (
	"strings"
	"testing"

	"github.com/dat267/pier/internal/markdown"
)

// The two shapes that dominate a transcript: prose (thinking blocks) and a reply
// with some inline markup.
var (
	benchProse = strings.Repeat(
		"Let me work through this carefully, weighing the alternatives and checking the edge cases as I go.\n", 40)
	benchMixed = strings.Repeat(
		"Here is the answer, with **emphasis** and `code` and a [link](https://example.com):\n\n- first point\n- second point\n\n```go\nfmt.Println(\"hi\")\n```\n", 8)
)

func BenchmarkLexMarkdownProse(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = markdown.Lex(benchProse)
	}
}

func BenchmarkLexMarkdownMixed(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = markdown.Lex(benchMixed)
	}
}

func benchmarkMarkdownRender(b *testing.B, text string) {
	b.Helper()
	theme := mdTestTheme()
	markdown := NewMarkdown(text, 1, 0, theme, nil, MarkdownOptions{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Invalidate each round so the work is measured rather than the cache.
		markdown.Invalidate()
		_ = markdown.Render(80)
	}
}

func BenchmarkMarkdownRenderProse(b *testing.B) { benchmarkMarkdownRender(b, benchProse) }

func BenchmarkMarkdownRenderMixed(b *testing.B) { benchmarkMarkdownRender(b, benchMixed) }

// An ANSI-styled theme, which is what the app uses: the tag theme above keeps
// the benchmark honest about plain text but not about the styled case the
// wrapper actually sees (an escape before and after every styled run).
func mdANSITestTheme() MarkdownTheme {
	wrap := func(code string) func(string) string {
		return func(text string) string { return "\x1b[" + code + "m" + text + "\x1b[0m" }
	}
	theme := mdTestTheme()
	theme.Heading = wrap("1;35")
	theme.Link = wrap("4;34")
	theme.LinkURL = wrap("2")
	theme.Code = wrap("36")
	theme.CodeBlock = wrap("32")
	theme.CodeBlockBorder = wrap("2")
	theme.Quote = wrap("3")
	theme.QuoteBorder = wrap("32")
	theme.Hr = wrap("2")
	theme.ListBullet = wrap("36")
	theme.Bold = wrap("1")
	theme.Italic = wrap("3")
	theme.Strikethrough = wrap("9")
	theme.Underline = wrap("4")
	return theme
}

func benchmarkMarkdownRenderANSI(b *testing.B, text string) {
	b.Helper()
	markdown := NewMarkdown(text, 1, 0, mdANSITestTheme(), nil, MarkdownOptions{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		markdown.Invalidate()
		_ = markdown.Render(80)
	}
}

func BenchmarkMarkdownRenderProseANSI(b *testing.B) { benchmarkMarkdownRenderANSI(b, benchProse) }

func BenchmarkMarkdownRenderMixedANSI(b *testing.B) { benchmarkMarkdownRenderANSI(b, benchMixed) }

// Streaming a message one paragraph at a time: the component carries the
// stable prefix forward (incremental lex plus the line cache), while the cold
// variant rebuilds the component every append and re-lexes the whole message.
func benchmarkMarkdownStream(b *testing.B, reuse bool) {
	b.Helper()
	theme := mdTestTheme()
	var prefix strings.Builder
	var prefixes []string
	for range 200 {
		prefix.WriteString("Paragraph with **bold** and `code` and a [link](https://example.com).\n\n")
		prefixes = append(prefixes, prefix.String())
	}
	if reuse {
		message := NewMarkdown("", 1, 0, theme, nil, MarkdownOptions{})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, text := range prefixes {
				message.SetText(text)
				_ = message.Render(80)
			}
		}
		return
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, text := range prefixes {
			message := NewMarkdown(text, 1, 0, theme, nil, MarkdownOptions{})
			_ = message.Render(80)
		}
	}
}

func BenchmarkMarkdownStreamAppend(b *testing.B)     { benchmarkMarkdownStream(b, true) }
func BenchmarkMarkdownStreamAppendCold(b *testing.B) { benchmarkMarkdownStream(b, false) }
