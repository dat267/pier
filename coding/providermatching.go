package coding

import (
	"strings"

	"github.com/dat267/pier/ai"
)

// FindMatchingProviders matches provider IDs and display names exactly, ignoring
// case and surrounding whitespace in the reference (pi v1.5.2
// core/provider-matching.ts findMatchingProviders).
func FindMatchingProviders(providers []*ai.Provider, providerRef string) []*ai.Provider {
	normalized := strings.ToLower(strings.TrimSpace(providerRef))
	if normalized == "" {
		return nil
	}
	var matches []*ai.Provider
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		if strings.ToLower(provider.ID) == normalized || strings.ToLower(provider.Name) == normalized {
			matches = append(matches, provider)
		}
	}
	return matches
}
