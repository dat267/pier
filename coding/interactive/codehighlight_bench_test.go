package interactive

import (
	"os"
	"testing"
)

// BenchmarkHighlightCodeGo measures the wired-in render path on a real Go
// source file. It is the guard that the highlight.js port, not Chroma,
// backs the per-fence work.
func BenchmarkHighlightCodeGo(b *testing.B) {
	code, err := os.ReadFile("../../internal/highlight/testdata/sample_big.go")
	if err != nil {
		b.Skip(err)
	}
	SetCustomThemesDir(b.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	InitTheme("dark", false)
	text := string(code)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = highlightCode(text, "go", ActiveTheme())
	}
}
