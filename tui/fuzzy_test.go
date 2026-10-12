package tui

import "testing"

// Upstream fuzzyMatch indexes by UTF-16 code units and treats every JS
// whitespace character as a word boundary.
func TestFuzzyMatchUsesUTF16IndicesAndWhitespace(t *testing.T) {
	// An astral character before the match is two code units, not one.
	if got, want := MatchFuzzy("a", "\U0001F600a").Score, unfusedProduct(2, 0.1); got != want {
		t.Fatalf("astral score = %v; want %v", got, want)
	}
	// NBSP is a JS whitespace boundary.
	if got, want := MatchFuzzy("a", "x\u00a0a").Score, unfusedProduct(2, 0.1)-10; got != want {
		t.Fatalf("nbsp score = %v; want %v", got, want)
	}
}
