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
}

func BenchmarkInlineUnmatchedBrackets(b *testing.B) {
	src := strings.Repeat("[", 32000)
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		ParseInline(src)
	}
}
