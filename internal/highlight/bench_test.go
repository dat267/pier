package highlight

import (
	"os"
	"testing"
)

func BenchmarkHighlightGo(b *testing.B) {
	code, err := os.ReadFile("testdata/sample_big.go")
	if err != nil {
		b.Skip(err)
	}
	text := string(code)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := Highlight(text, "go")
		if err != nil {
			b.Fatal(err)
		}
		_ = RenderHTML(result.Nodes)
	}
}

func BenchmarkHighlightGoNodes(b *testing.B) {
	code, _ := os.ReadFile("testdata/sample_big.go")
	text := string(code)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Highlight(text, "go")
	}
}
