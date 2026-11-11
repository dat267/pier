package coding

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
	switch {
	case aName < bName:
		return -1
	case aName > bName:
		return 1
	}
	return 0
}
