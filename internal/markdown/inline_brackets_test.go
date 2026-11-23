package markdown

import (
	"strings"
	"testing"
)

// A link requires "](" after its opener and a later ")" for the target, so a
// run of unmatched brackets cannot be one. Marked rejects them without
// rescanning the tail per character (M23); the port now does the same.
func TestInlineUnmatchedBracketsStayText(t *testing.T) {
	src := strings.Repeat("[", 32000)
	if got := ParseInline(src); len(got) != 1 || got[0].Kind != InlineText || got[0].Text != src {
		t.Fatalf("unmatched brackets did not stay literal text: %d inlines", len(got))
	}
	// A bracket run without a closing parenthesis is not a link either.
	open := strings.Repeat("[a", 8000) + "]("
	if got := ParseInline(open); len(got) != 1 || got[0].Kind != InlineText || got[0].Text != open {
		t.Fatalf("bracket run without a target close did not stay literal text: %d inlines", len(got))
	}
	// The guard must not reject real links beside brackets.
	link := "[[" + strings.Repeat("[", 8000) + "l](u)"
	if got := ParseInline(link); len(got) == 0 || got[len(got)-1].Kind != InlineLink || got[len(got)-1].Dest != "u" {
		t.Fatalf("guard rejected a real link: %+v", got)
	}
	// A stray "](" after a link's target leaves the link standing: marked
	// 18.0.11 lexes `[t](u)](` as a link and the text `](` (pi v1.6.0,
	// faac5858). The guard used the last "](" and found no ")" after it.
	for _, src := range []string{"[t](u)](", "![i](p))](", "[t](u)](x"} {
		if got := ParseInline(src); len(got) != 2 || (got[0].Kind != InlineLink && got[0].Kind != InlineImage) || got[1].Kind != InlineText {
			t.Fatalf("ParseInline(%q) = %+v, want a link and text", src, got)
		}
	}
	// A ")" before the last "](" alone does not make a bracket run a link.
	closedEarly := strings.Repeat("[a", 8000) + ")]("
	if got := ParseInline(closedEarly); len(got) != 1 || got[0].Kind != InlineText {
		t.Fatalf("bracket run with an earlier \")\" did not stay literal text: %d inlines", len(got))
	}
}

func BenchmarkInlineUnmatchedBrackets(b *testing.B) {
	src := strings.Repeat("[", 32000)
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		ParseInline(src)
	}
}
