package tui

// Benchmarks for the LaTeX renderer. D466 records the measured gap against the
// pinned reference and the two scans these keep honest: a nested script chain
// (the pathological case that made the relation regexp quadratic) and a
// display block (the representative case that spends its time in the parser).

import (
	"strings"
	"testing"
)

func benchmarkRenderLatex(b *testing.B, source string, options RenderLatexOptions) {
	b.ReportAllocs()
	for b.Loop() {
		RenderLatex(source, options)
	}
}

// BenchmarkRenderLatexNestedScriptsInline renders x^{^{...y...}}; each level
// re-normalizes the value it received, so the no-relation scan path decides
// whether the render is linear or quadratic in the nesting depth.
func BenchmarkRenderLatexNestedScriptsInline(b *testing.B) {
	source := "x" + strings.Repeat("^{", 200) + "y" + strings.Repeat("}", 200)
	benchmarkRenderLatex(b, source, RenderLatexOptions{})
}

// BenchmarkRenderLatexDisplayMath renders an aligned block whose script values
// carry relation characters, so normalization takes the rewriting path.
func BenchmarkRenderLatexDisplayMath(b *testing.B) {
	source := `\begin{aligned} a &= b + c \\ d &= e \end{aligned}`
	benchmarkRenderLatex(b, source, RenderLatexOptions{Display: true})
}

// BenchmarkRenderLatexInlineFormula covers the common two-term inline formula.
func BenchmarkRenderLatexInlineFormula(b *testing.B) {
	benchmarkRenderLatex(b, `\frac{a+b}{c} \cdot \sqrt{x^2 + y^2}`, RenderLatexOptions{})
}
