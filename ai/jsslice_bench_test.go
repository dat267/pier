package ai

import (
	"strings"
	"testing"
)

// BenchmarkJSLower keeps D518 visible: mixed-case and lowercase ASCII take the
// fast path, and accented text the full Unicode mapping.
func BenchmarkJSLower(b *testing.B) {
	for name, input := range map[string]string{
		"ascii":     "anthropic/Claude-Sonnet-4-5-20250929",
		"lowercase": "anthropic/claude-sonnet-4-5-20250929",
		"nonascii":  "Café-Résumé/Claude-Sonnet",
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = JSLower(input)
			}
		})
	}
}

func BenchmarkJSSliceASCII(b *testing.B) {
	s := strings.Repeat("a", 1<<20)
	b.SetBytes(int64(len(s)))

	for b.Loop() {
		_ = JSSlice(s, 0, 4000)
	}
}

func BenchmarkJSSliceUnicode(b *testing.B) {
	s := strings.Repeat("é", 1<<19)
	b.SetBytes(int64(len(s)))

	for b.Loop() {
		_ = JSSlice(s, 0, 4000)
	}
}

func BenchmarkGoSlicePrefixBaseline(b *testing.B) {
	s := strings.Repeat("a", 1<<20)
	b.SetBytes(int64(len(s)))

	for b.Loop() {
		_ = s[:4000]
	}
}
