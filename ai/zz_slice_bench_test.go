package ai

import (
	"strings"
	"testing"
)

func BenchmarkZZJSSliceASCII(b *testing.B) {
	s := strings.Repeat("a", 1<<20)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = JSSlice(s, 0, 4000)
	}
}

func BenchmarkZZJSSliceUnicode(b *testing.B) {
	s := strings.Repeat("é", 1<<19)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = JSSlice(s, 0, 4000)
	}
}

func BenchmarkZZGoSlicePrefix(b *testing.B) {
	s := strings.Repeat("a", 1<<20)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s[:4000]
	}
}
