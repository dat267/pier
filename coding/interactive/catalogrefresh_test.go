package interactive

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// D456: the reference registers a failed provider in a Map as each concurrent
// Promise.all refresh settles (packages/ai/src/models.ts:603), so the order it
// then lists is completion order. The port sorts instead, which is what every
// "Could not refresh …" message relies on.
func TestModelRefreshErrorProvidersAreSorted(t *testing.T) {
	failures := map[string]error{}
	for _, provider := range []string{"openai", "github-copilot", "openrouter", "anthropic", "google", "azure", "zai"} {
		failures[provider] = errors.New("refresh failed")
	}
	want := []string{"anthropic", "azure", "github-copilot", "google", "openai", "openrouter", "zai"}
	if got := errorProviders(failures); !slices.Equal(got, want) {
		t.Fatalf("providers = %q, want %q", got, want)
	}
	// The joined message is what the user sees; it must name every provider once.
	if joined := strings.Join(errorProviders(failures), ", "); joined != strings.Join(want, ", ") {
		t.Fatalf("joined = %q", joined)
	}
	if providers := errorProviders(nil); len(providers) != 0 {
		t.Fatalf("no failures = %q, want empty", providers)
	}
}
