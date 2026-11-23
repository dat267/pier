package coding

import (
	"sync"
	"testing"
)

// pi v1.5.2 b4afdddf core/preferred-providers.ts compareProviders orders
// non-preferred providers by `a.name.localeCompare(b.name)`, which sorts
// letters before accented variants of other letters ("Äpfel" before "Zebra").
// A byte comparison puts every non-ASCII name after the ASCII ones.
func TestComparePreferredProvidersUsesCollation(t *testing.T) {
	if got := ComparePreferredProviders("custom-a", "Zebra", "custom-b", "Äpfel"); got <= 0 {
		t.Fatalf("ComparePreferredProviders(Zebra, Äpfel) = %d, want Zebra after Äpfel", got)
	}
	if got := ComparePreferredProviders("custom-a", "Äpfel", "custom-b", "Zebra"); got >= 0 {
		t.Fatalf("ComparePreferredProviders(Äpfel, Zebra) = %d, want Äpfel before Zebra", got)
	}
	// Preferred partners still come first, and equal names compare equal.
	if got := ComparePreferredProviders("radius", "Zebra", "custom-b", "Äpfel"); got >= 0 {
		t.Fatalf("preferred provider did not sort first: %d", got)
	}
	if got := ComparePreferredProviders("custom-a", "Same", "custom-b", "Same"); got != 0 {
		t.Fatalf("equal names = %d, want 0", got)
	}
}

// ComparePreferredProviders is reached from the off-loop /login argument
// completion (tui Editor runs GetSuggestions in a goroutine) and from the
// on-loop auth selector, so the shared collator it compares with must
// tolerate concurrent calls; x/text collators reuse internal iterators.
func TestComparePreferredProvidersIsSafeForConcurrentUse(t *testing.T) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 200 {
				if ComparePreferredProviders("a", "Ålborg", "b", "zeta") >= 0 {
					t.Error("Ålborg must sort before zeta")
					return
				}
			}
		})
	}
	wg.Wait()
}
