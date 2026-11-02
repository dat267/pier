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

func TestMatchFuzzyASCIISwapFallbackDoesNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(10, func() {
		match := MatchFuzzy("a2", "2ab")
		if !match.Matches || match.Score != -19.9 {
			t.Fatalf("match = %+v, want a match with score -19.9", match)
		}
	})
	if allocations != 0 {
		t.Fatalf("ASCII alpha-numeric swap allocated %.0f objects, want 0", allocations)
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

// pi v1.5.0 8c03fef4d (#11220): the greedy alignment can scatter the query over
// an earlier unrelated part of the text, so every contiguous occurrence is
// scored and the best alignment wins ("opus" must not match via "openRouter").
func TestFuzzyMatchRanksContiguousOccurrences(t *testing.T) {
	cases := []struct {
		query   string
		scatter string
	}{
		{"opus", "openrouter openrouter/anthropic/claude-opus"},
		{"astra", "radius radius/gpt astra-1"},
		{"gpt", "google openai gpt-4o openrouter"},
	}
	for _, probe := range cases {
		scattered := MatchFuzzy(probe.query, probe.scatter)
		if !scattered.Matches {
			t.Fatalf("%q did not match %q", probe.query, probe.scatter)
		}
		// A wholly contiguous match always scores better than the scattered one.
		contiguous := MatchFuzzy(probe.query, probe.query+"-tail")
		if contiguous.Score >= scattered.Score {
			t.Fatalf("%q: contiguous score %v, scattered %v", probe.query, contiguous.Score, scattered.Score)
		}
	}
}
