package coding

import (
	"math"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.5.0 core/cache-warmer.ts lastPromptTokens returns the Number sum of
// the latest assistant input, cacheRead and cacheWrite. The Go port must
// saturate that sum rather than wrapping it negative.
func TestLastCacheWarmingPromptTokensSaturatesHugeUsage(t *testing.T) {
	entry := func(usage ai.Usage) SessionEntry {
		raw, err := ai.MarshalMessage(&ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
			Usage: usage,
		})
		if err != nil {
			t.Fatal(err)
		}
		return SessionEntry{Type: "message", Message: raw}
	}
	if got, want := lastCacheWarmingPromptTokens([]SessionEntry{entry(ai.Usage{Input: 100, CacheRead: 20, CacheWrite: 5})}), int64(125); got != want {
		t.Fatalf("ordinary prompt tokens = %d, want %d", got, want)
	}
	huge := ai.Usage{Input: 6e18, CacheRead: 6e18, CacheWrite: 6e18}
	if got, want := lastCacheWarmingPromptTokens([]SessionEntry{entry(huge)}), int64(math.MaxInt64); got != want {
		t.Fatalf("huge prompt tokens = %d, want %d", got, want)
	}
}
