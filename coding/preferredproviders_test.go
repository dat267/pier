package coding

import "testing"

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
