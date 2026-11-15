package interactive

import "testing"

// pi v1.5.0, model-search.ts getDistinctModelName: isCovered tests
// `word.length > 1`, JavaScript string length, so a single astral letter counts
// as two and can be covered by a compact id. Counting runes made such a name
// distinct when the reference hides it.
func TestDistinctModelNameCountsUTF16Units(t *testing.T) {
	if name, ok := GetDistinctModelName("a𝔘b", "𝔘"); ok {
		t.Fatalf("name %q reported distinct, want the astral letter covered by the compact id", name)
	}
	if name, ok := GetDistinctModelName("a𝔘b", "x"); !ok || name != "x" {
		t.Fatalf("name %q ok=%v, want a distinct name", name, ok)
	}
}
