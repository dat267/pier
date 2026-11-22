package markdown

import (
	"strings"
	"testing"
)

// testTerminal drives the parity renderer with the package's own layout
// primitives, so the renderer tests need no terminal library.
type testTerminal struct{}

func (testTerminal) Wrap(text string, width int) []string { return wrapStyled(text, width) }
func (testTerminal) VisibleWidth(text string) int         { return visibleWidth(text) }
func (testTerminal) ApplyBackground(line string, width int, bg func(string) string) string {
	return bg(line)
}
func (testTerminal) IsImageLine(string) bool                  { return false }
func (testTerminal) Hyperlink(text string, url string) string { return text }

func renderBlocksTest(t *testing.T, src string, width int, opts Options) []string {
	t.Helper()
	if opts.Terminal == nil {
		opts.Terminal = testTerminal{}
	}
	renderer := NewRenderer(opts)
	blocks := Lex(src)
	blocks = blocks[:len(blocks):len(blocks)]
	var lines []string
	for i, b := range blocks {
		var next *Block
		if i+1 < len(blocks) {
			next = blocks[i+1]
		}
		lines = append(lines, renderer.Finalize(renderer.RenderBlock(b, width, next), width, width, 0)...)
	}
	return lines
}

// TestRendererPendingInlineLatexRendersRaw: an unterminated `$…` that looks like
// math is emitted by the lexer as a pending LaTeX span, and the renderer must
// show its raw source instead of asking the LaTeX renderer (upstream
// markdown.ts marks it pending for exactly this reason).
func TestRendererPendingInlineLatexRendersRaw(t *testing.T) {
	opts := Options{LatexEnabled: true, Latex: func(source string, display bool) (string, bool) {
		return "RENDERED", true
	}}
	got := renderBlocksTest(t, "a $x^2", 40, opts)
	if len(got) == 0 || strings.TrimRight(got[0], " ") != "a $x^2" {
		t.Fatalf("pending latex = %q, want the raw source", got)
	}
}

// TestRendererClosedInlineLatexUsesRenderer is the control: a closed span is
// rendered.
func TestRendererClosedInlineLatexUsesRenderer(t *testing.T) {
	opts := Options{LatexEnabled: true, Latex: func(source string, display bool) (string, bool) {
		return "RENDERED(" + source + ")", true
	}}
	got := renderBlocksTest(t, "a $x^2$ b", 40, opts)
	if len(got) == 0 || strings.TrimRight(got[0], " ") != "a RENDERED(x^2) b" {
		t.Fatalf("closed latex = %q, want the rendered form", got)
	}
}

// TestRendererPendingBlockLatexRendersRaw covers the display-math side.
func TestRendererPendingBlockLatexRendersRaw(t *testing.T) {
	opts := Options{LatexEnabled: true, Latex: func(source string, display bool) (string, bool) {
		return "RENDERED", true
	}}
	got := renderBlocksTest(t, "$$\na+b\n", 40, opts)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "$$") || strings.Contains(joined, "RENDERED") {
		t.Fatalf("pending block latex = %q, want the raw source", got)
	}
}

// pi v1.5.2 (b4afdddf) packages/tui/src/utils.ts wrapSingleLine breaks a token
// wider than the width character by character (`breakLongWord`), styled or not.
// This package's styled path only broke before a token, so a styled word longer
// than the width overflowed the line, while the escape-free path (wrapPlain) did
// break it. RenderSource is a public entry point, so the overflow is reachable.
func TestStyledWrapBreaksAWordWiderThanTheWidth(t *testing.T) {
	for _, line := range []string{"\x1b[31mabcdefghij\x1b[39m", "abcdefghij"} {
		got := wrapStyled(line, 4)
		for _, l := range got {
			if w := visibleWidth(l); w > 4 {
				t.Fatalf("wrapStyled(%q, 4) produced a %d-wide line %q from %q", line, w, l, got)
			}
		}
		if len(got) != 3 {
			t.Fatalf("wrapStyled(%q, 4) = %q, want three lines like [abcd efgh ij]", line, got)
		}
	}
}
