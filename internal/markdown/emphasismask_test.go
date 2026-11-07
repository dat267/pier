package markdown

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// maskEmphasisSourceReference is the straightforward form of the Marked 18
// Lexer.inlineTokens anyPunctuation and blockSkip masks that
// maskEmphasisSource replaced: mask escapes in a byte copy, snapshot it, then
// mask code spans, `<...>` runs and inline links while reading the snapshot.
func maskEmphasisSourceReference(src string) string {
	masked := []byte(src)
	for i := 0; i+1 < len(src); i++ {
		if src[i] != '\\' {
			continue
		}
		next, size := utf8.DecodeRuneInString(src[i+1:])
		if unicode.IsPunct(next) || unicode.IsSymbol(next) {
			for end := i + 1 + size; i < end; i++ {
				masked[i] = '+'
			}
			i--
		}
	}
	source := string(masked)
	for i := 0; i < len(source); i++ {
		end := -1
		switch source[i] {
		case '`':
			if i == 0 || source[i-1] != '`' {
				end = emphasisCodeMaskEnd(source, i)
			}
		case '<':
			if i+1 < len(source) && source[i+1] != ' ' {
				if offset := strings.IndexAny(source[i+1:], "<>"); offset >= 0 && source[i+1+offset] == '>' {
					end = i + offset + 2
				}
			}
		case '[':
			end = emphasisLinkMaskEnd(source, i)
		}
		if end > i {
			masked[i] = '['
			for j := i + 1; j < end-1; j++ {
				masked[j] = 'a'
			}
			masked[end-1] = ']'
			i = end - 1
		}
	}
	return string(masked)
}

// The copy-on-write masks must produce the reference bytes, including masks
// that touch (an escape before a code span, adjacent spans, a backtick right
// after a masked span) and multi-byte escaped symbols.
func TestMaskEmphasisSourceMatchesReference(t *testing.T) {
	cases := []string{
		"", "plain words", "**bold** and _em_", "\\*not em\\*", "a \\€ b", "\\é not punct",
		"`code *x*` and *y*", "``a`b`` *c*", "`a``b`", "<https://x.y/*z*> *w*", "< not>",
		"[a *b*](https://c/(d)) *e*", "[a](b(c(d))) *f*", "[x][y]", "\\`[a](b)`c`", "`a`[b](c)`d`",
		"\\\\*x*", "[`a]`](b) *c*", "<a>`b`<c>", "\\[a](b) *c*", "trailing \\",
	}
	rng := rand.New(rand.NewPCG(1, 2))
	const alphabet = "ab *_`\\[]()<>!€é\n"
	runes := []rune(alphabet)
	for range 4000 {
		var b strings.Builder
		for range rng.IntN(24) {
			b.WriteRune(runes[rng.IntN(len(runes))])
		}
		cases = append(cases, b.String())
	}
	for _, src := range cases {
		if got, want := maskEmphasisSource(src), maskEmphasisSourceReference(src); got != want {
			t.Fatalf("maskEmphasisSource(%q) = %q; want %q", src, got, want)
		}
	}
}

// A span with nothing to mask is returned as is, and one that only masks
// spans builds its result once.
func TestMaskEmphasisSourceCopiesOnlyWhenMasking(t *testing.T) {
	plain := "Paragraph with **bold** and some _emphasis_ but no spans."
	if n := testing.AllocsPerRun(100, func() { _ = maskEmphasisSource(plain) }); n != 0 {
		t.Fatalf("unmasked span: %v allocs; want 0", n)
	}
	spans := "Paragraph with **bold** and `code` and a [link](https://example.com)."
	if n := testing.AllocsPerRun(100, func() { _ = maskEmphasisSource(spans) }); n != 1 {
		t.Fatalf("masked span: %v allocs; want 1", n)
	}
}
