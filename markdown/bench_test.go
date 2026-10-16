package markdown

import (
	"fmt"
	"strings"
	"testing"
)

func benchProse(lines int) string {
	return strings.Repeat("Let me work through this carefully, weighing the alternatives and checking the edge cases as I go.\n", lines)
}

func benchMixed(blocks int) string {
	return strings.Repeat("Here is the answer, with **emphasis** and `code` and a [link](https://example.com):\n\n- first point\n- second point\n\n```go\nfmt.Println(\"hi\")\n```\n", blocks)
}

func benchDoc(bytes int) string {
	var b strings.Builder
	for b.Len() < bytes {
		fmt.Fprintf(&b, "## Section %d\n\nSome prose with **bold**, *italic* and `inline code` and a [link](https://example.com).\n\n", b.Len())
		b.WriteString("- list item one\n- list item two\n\n")
		b.WriteString("```go\nfunc example() error {\n\tfor i := 0; i < 10; i++ {\n\t\tfmt.Println(i)\n\t}\n\treturn nil\n}\n```\n\n")
	}
	return b.String()
}

func BenchmarkLexProse40(b *testing.B)  { benchmarkLex(b, benchProse(40)) }
func BenchmarkLexProse500(b *testing.B) { benchmarkLex(b, benchProse(500)) }
func BenchmarkLexMixed8(b *testing.B)   { benchmarkLex(b, benchMixed(8)) }
func BenchmarkLexDoc64KB(b *testing.B)  { benchmarkLex(b, benchDoc(64<<10)) }
func BenchmarkLexDoc256KB(b *testing.B) { benchmarkLex(b, benchDoc(256<<10)) }
func BenchmarkLexDoc1MB(b *testing.B)   { benchmarkLex(b, benchDoc(1<<20)) }

func benchmarkLex(b *testing.B, text string) {
	b.Helper()
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Lex(text)
	}
}

// BenchmarkInlineIsolated measures the inline stage alone on the same text.
func BenchmarkInlineIsolated(b *testing.B) {
	text := "Here is the answer, with **emphasis** and `code` and a [link](https://example.com):"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ParseInline(text)
	}
}
