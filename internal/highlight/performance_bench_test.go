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

// Compare the unchanged, unbatched scan loop with the optimized scanner in the
// same process to separate representative-workload regressions from CPU drift.
func BenchmarkIgnoredIllegalScanner(b *testing.B) {
	for _, tc := range []struct{ name, language, code string }{
		{"repeated", "json", strings.Repeat("0x", 4096)},
		{"json", "json", strings.Repeat(`{"key":"value","number":123},`, 256)},
		{"valid-json", "json", "[" + strings.TrimSuffix(strings.Repeat(`{"key":"value","number":123},`, 256), ",") + "]"},
		{"javascript", "javascript", strings.Repeat("const x = 42; // comment\n", 128)},
		{"python", "python", strings.Repeat("def run(x):\n    return x + 42 # comment\n", 128)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			original := languageWithoutIllegalBatch(b, tc.language)
			batched, err := getCompiledLanguage(tc.language)
			if err != nil {
				b.Fatal(err)
			}
			for _, variant := range []struct {
				name string
				lang *compiledLanguage
			}{{"unbatched", original}, {"batched", batched}} {
				b.Run(variant.name, func(b *testing.B) {
					_ = highlightWith(variant.lang, tc.code, true)
					b.ReportAllocs()
					for b.Loop() {
						_ = highlightWith(variant.lang, tc.code, true)
					}
				})
			}
		})
	}
}

func BenchmarkHighlightJSONDocument(b *testing.B) {
	code := strings.Repeat(`{"key":"value","number":123},`, 256) // 7.25 KiB
	b.ReportAllocs()
	for b.Loop() {
		if _, err := HighlightWith(code, "json", true); err != nil {
			b.Fatal(err)
		}
	}
}

// A long run with no matching rule made each dispatched candidate rescan the
// tail; the rejection budget bounds it (D351).
func BenchmarkHighlightRejectedRun(b *testing.B) {
	code := strings.Repeat("0x", 4*1024) // 8 KiB, no JavaScript rule matches
	b.ReportAllocs()
	for b.Loop() {
		if _, err := HighlightWith(code, "javascript", true); err != nil {
			b.Fatal(err)
		}
	}
}
