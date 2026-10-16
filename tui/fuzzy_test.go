package tui

import "testing"

func TestMatchFuzzyASCIIPathDoesNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(10, func() {
		match := MatchFuzzy("alpha", "src/alphabeta.txt")
		if !match.Matches {
			t.Fatal("ASCII query did not match")
		}
	})
	if allocations != 0 {
		t.Fatalf("ASCII fuzzy match allocated %.0f objects, want 0", allocations)
	}
}

func TestMatchFuzzyASCIINoMatchPathDoesNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(10, func() {
		match := MatchFuzzy("NoMatch", "src/file.txt")
		if match.Matches {
			t.Fatal("unexpected match")
		}
	})
	if allocations != 0 {
		t.Fatalf("ASCII non-match allocated %.0f objects, want 0", allocations)
	}
}

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
