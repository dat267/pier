package coding

import (
	"strings"
	"testing"
)

// BenchmarkSliceHead keeps D517 visible: a 2,000-unit head of a megabyte line,
// ASCII (grep output) and mixed (accented and astral characters).
func BenchmarkSliceHead(b *testing.B) {
	for name, line := range map[string]string{
		"ascii": strings.Repeat("hello world, some grep output line ", 30000),
		"mixed": strings.Repeat("héllo wörld 😀 ", 70000),
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = SliceHead(line, 2000)
			}
		})
	}
}

// BenchmarkTruncateForSummary covers the compaction path, which also reports
// how many characters it cut.
func BenchmarkTruncateForSummary(b *testing.B) {
	text := strings.Repeat("héllo wörld 😀 ", 70000)
	b.ReportAllocs()
	for b.Loop() {
		_ = truncateForSummary(text, toolResultMaxChars)
	}
}
