package highlight

import (
	"strings"
	"testing"
)

// Same-workload benchmarks track the reproduced performance gap against pi
// v1.5.0 packages/coding-agent/src/utils/syntax-highlight.ts (M140).
func BenchmarkHighlightRepeatedHexText(b *testing.B) {
	code := strings.Repeat("0x", 4*1024) // 8 KiB
	b.ReportAllocs()
	for b.Loop() {
		if _, err := HighlightWith(code, "json", true); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHighlightJSONDocument(b *testing.B) {
	code := strings.Repeat(`{"key":"value","number":123},`, 256) // 8 KiB
	b.ReportAllocs()
	for b.Loop() {
		if _, err := HighlightWith(code, "json", true); err != nil {
			b.Fatal(err)
		}
	}
}
