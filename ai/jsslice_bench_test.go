package ai

import (
	"strings"
	"testing"
)

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
