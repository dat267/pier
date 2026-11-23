package coding

import (
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Port of pi v1.5.2 (b4afdddf) core/preferred-providers.ts: the preferred
// partner providers and the order provider lists show them in.

// PreferredProviderIDs are the preferred partner providers, most preferred
// first.
var PreferredProviderIDs = []string{"radius", "openrouter"}

// IsPreferredProvider reports whether the provider is a preferred partner.
func IsPreferredProvider(providerID string) bool {
	return PreferredProviderRank(providerID) < len(PreferredProviderIDs)
}

// PreferredProviderRank is the provider's position among the preferred partners,
// or len(PreferredProviderIDs) for any other provider.
func PreferredProviderRank(providerID string) int {
	for index, id := range PreferredProviderIDs {
		if id == providerID {
			return index
		}
	}
	return len(PreferredProviderIDs)
}

// ComparePreferredProviders orders providers: preferred partners first, in their
// order, then all others by name.
func ComparePreferredProviders(aID, aName, bID, bName string) int {
	rankA := PreferredProviderRank(aID)
	rankB := PreferredProviderRank(bID)
	if rankA != rankB {
		return rankA - rankB
	}
	// D257: follow localeCompare rather than byte ordering, so an accented
	// name sorts next to its base letter instead of after every ASCII name.
	return CompareRootCollation(aName, bName)
}

// rootCollation is the root collation `localeCompare` uses without the host
// locale (D257). One collator is shared, because building one per comparison
// cost 1.25 us and 5 KB and the footer sorts its status keys on every frame;
// a Collator keeps scratch iterators, so the shared instance is used under a
// lock. Sorts reach it both on the UI loop and off it (the /login argument
// completion runs in the editor's autocomplete goroutine).
var rootCollation struct {
	once sync.Once
	mu   sync.Mutex
	coll *collate.Collator
}

// CompareRootCollation orders two strings like JavaScript's default
// `a.localeCompare(b)`, using root collation rather than the host locale.
func CompareRootCollation(a, b string) int {
	rootCollation.once.Do(func() { rootCollation.coll = collate.New(language.Und) })
	rootCollation.mu.Lock()
	defer rootCollation.mu.Unlock()
	return rootCollation.coll.CompareString(a, b)
}
