package markdown

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// The incremental lexer's contract is that Update(prefix) equals Lex(prefix)
// for every prefix of a growing source. These fragments are the block-level
// constructs whose interaction with an append is not obvious: an unfinished
// block can be reinterpreted (a pending `$$` block reaching back over a blank
// line, A528; a setext underline absorbing a line an interrupt already claimed,
// A529), and the combinations below cover them in short documents.
var incrementalHazardFragments = []string{
	"$$",
	"\\[",
	"\\(",
	"```",
	"~~~",
	"> ",
	"- ",
	"1. ",
	"| a |",
	"|---|",
	"---",
	"===",
	"<!--",
	"*a*",
	"text",
	"",
	"    code",
	"<div>",
	"$$x$$",
	"\\]",
	"-->",
}

// incrementalHazardCorpus builds the documents the invariant is checked on:
// every ordered pair of fragments, then a seeded sample of triples, so a hidden
// interaction between three constructs is still reachable.
func incrementalHazardCorpus() []string {
	corpus := make([]string, 0, len(incrementalHazardFragments)*len(incrementalHazardFragments)+(1<<10))
	for _, first := range incrementalHazardFragments {
		for _, second := range incrementalHazardFragments {
			corpus = append(corpus, first+"\n"+second)
		}
	}
	rng := rand.New(rand.NewSource(529))
	for i := 0; i < 1<<10; i++ {
		fragments := make([]string, 3)
		for j := range fragments {
			fragments[j] = incrementalHazardFragments[rng.Intn(len(incrementalHazardFragments))]
		}
		corpus = append(corpus, strings.Join(fragments, "\n"))
	}
	return corpus
}

func TestIncrementalMatchesFullLexForHazards(t *testing.T) {
	corpus := incrementalHazardCorpus()
	if len(corpus) < 1<<10 {
		t.Fatalf("corpus shrank to %d documents", len(corpus))
	}
	for _, source := range corpus {
		doc := NewDocument()
		for n := 1; n <= len(source); n++ {
			if got, want := doc.Update(source[:n]), Lex(source[:n]); !reflect.DeepEqual(got, want) {
				t.Fatalf("%q prefix %d:\n got %v\nwant %v", source, n, blockKinds(got), blockKinds(want))
			}
		}
	}
}
