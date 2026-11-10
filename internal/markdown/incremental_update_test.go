package markdown

import (
	"math/rand"
	"testing"
)

// A growing document must lex to the same blocks as a fresh full lex, at every
// intermediate length. Reference: the incremental pass exists only as a fast
// path (no upstream counterpart), so the invariant is local: Document.Update is
// required to agree with Lex on the same source.
func TestDocumentUpdateMatchesFullLex(t *testing.T) {
	pieces := []string{
		"# h\n", "para\n", "\n", "- item\n", "1. one\n", "> quote\n",
		"```\ncode\n```\n", "| a | b |\n|---|---|\n| 1 | 2 |\n",
		"[ref]: /u\n", "- [ ] task\n", "text `code` more\n", "  indented\n",
		"***\n", "> - nested\n", "\t\ttab\n", "a [ref] b\n",
	}
	rng := rand.New(rand.NewSource(11))
	compare := func(src string, got, want []*Block) {
		if len(got) != len(want) {
			t.Fatalf("src=%q: block count %d != %d\nwant=%q\ngot=%q", src, len(want), len(got), raws(want), raws(got))
		}
		for i := range want {
			if got[i].Kind != want[i].Kind || got[i].Raw != want[i].Raw ||
				got[i].Start != want[i].Start || got[i].End != want[i].End {
				t.Fatalf("src=%q block %d: got {%v %q %d-%d} want {%v %q %d-%d}",
					src, i, got[i].Kind, got[i].Raw, got[i].Start, got[i].End,
					want[i].Kind, want[i].Raw, want[i].Start, want[i].End)
			}
		}
	}
	for iter := 0; iter < 400; iter++ {
		n := 1 + rng.Intn(10)
		doc := &Document{}
		src := ""
		for i := 0; i < n; i++ {
			src += pieces[rng.Intn(len(pieces))]
			got := doc.Update(src)
			compare(src, got, Lex(src))
		}
	}
}

func raws(blocks []*Block) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.Raw)
	}
	return out
}
