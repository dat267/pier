package interactive

import (
	"strings"
	"testing"
)

// wordDiffLargeInput is a 2,000-word line with two changed words, the shape the
// diff renderer sees when a long line is edited.
func wordDiffLargeInput() (string, string) {
	words := make([]string, 2000)
	for i := range words {
		words[i] = "word" + strings.Repeat("x", i%5)
	}
	oldText := strings.Join(words, " ")
	words[1000] = "changed"
	words[1500] = "different"
	return oldText, strings.Join(words, " ")
}

// jsdiff 8.0.4 tokenizes with one regex match and array joins; the port must
// not run a regex per token (M142).
func TestDiffWordTokenizerAllocationBudget(t *testing.T) {
	oldText, newText := wordDiffLargeInput()
	allocs := testing.AllocsPerRun(3, func() { _ = DiffWords(oldText, newText) })
	if allocs > 1000 {
		t.Fatalf("word diff allocated %.0f objects, want <= 1000", allocs)
	}
}

func BenchmarkDiffWordsLarge(b *testing.B) {
	oldText, newText := wordDiffLargeInput()
	b.ReportAllocs()
	for b.Loop() {
		_ = DiffWords(oldText, newText)
	}
}

// The scanner and jsdiff's tokenizer must both reconstruct the source through
// WordDiff.join; this catches split or merge mistakes the small corpus misses.
func FuzzWordTokenizeRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "a", "a b", " a  b ", "a-b", "- - -", "a\u00a0\u00a0b", "a\u3000b", "é😀x", "a\tb\nc"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if got := diffJoin(diffWordTokenize(value)); got != value {
			t.Fatalf("round trip %q = %q", value, got)
		}
	})
}
