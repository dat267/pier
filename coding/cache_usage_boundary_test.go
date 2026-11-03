package coding

import (
	"math"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.0 (f9a26365), coding-agent/src/core/cache-stats.ts
// detectMiss/asPreviousRequest retain Number-space prompt sums until the miss
// is known. Only the public int64 result needs representation saturation.
func TestCacheMissDoesNotLoseHugePositivePromptUsage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage ai.Usage
		want  int64
	}{
		{name: "wide prompt", usage: ai.Usage{Input: 6000000000000000000, CacheRead: 6000000000000000000}, want: 6000000000000000000},
		{name: "wide miss", usage: ai.Usage{Input: 6000000000000000000, CacheRead: 6000000000000000000, CacheWrite: 6000000000000000000}, want: math.MaxInt64},
		{name: "ordinary", usage: ai.Usage{Input: 20000, CacheRead: 10000}, want: 20000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, _ := newTestSession(t)
			previous := &ai.AssistantMessage{Provider: "anthropic", Model: "fixture", StopReason: ai.StopStop, Usage: tc.usage}
			manager.AppendMessage(previous)
			current := *previous
			for name, get := range map[string]func() (CacheMiss, bool){
				"reference": func() (CacheMiss, bool) { return DetectCacheMiss(manager.GetEntries(), &current, nil) },
				"cached":    func() (CacheMiss, bool) { return manager.CacheMissFor(&current, nil) },
			} {
				t.Run(name, func(t *testing.T) {
					miss, ok := get()
					if !ok || miss.MissedTokens != tc.want {
						t.Fatalf("cache miss = %+v, counted=%v, want %d positive missed tokens", miss, ok, tc.want)
					}
				})
			}
		})
	}
}

// Reference: pi v1.5.0 (f9a26365), cache-stats.ts computes paidPerToken
// from input + cacheWrite before applying it to missedTokens.
func TestCacheMissKeepsPaidRateWhenTokenSumExceedsInt64(t *testing.T) {
	manager, _ := newTestSession(t)
	manager.AppendMessage(&ai.AssistantMessage{
		Provider: "anthropic", Model: "fixture", StopReason: ai.StopStop,
		Usage: ai.Usage{CacheWrite: 6000000000000000000},
	})
	current := &ai.AssistantMessage{
		Provider: "anthropic", Model: "fixture", StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 6000000000000000000, CacheWrite: 6000000000000000000, Cost: ai.UsageCost{Input: 6, CacheWrite: 6, Total: 12}},
	}
	for name, get := range map[string]func() (CacheMiss, bool){
		"reference": func() (CacheMiss, bool) { return DetectCacheMiss(manager.GetEntries(), current, nil) },
		"cached":    func() (CacheMiss, bool) { return manager.CacheMissFor(current, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			miss, ok := get()
			if !ok || math.Abs(miss.MissedCost-6) > 1e-12 {
				t.Fatalf("cache miss = %+v, counted=%v, want extra cost 6", miss, ok)
			}
		})
	}
}

// Reference: pi v1.5.0 (f9a26365), cache-stats.ts asPreviousRequest
// remembers cache activity when cacheRead + cacheWrite is positive.
func TestCacheMissRemembersHugePositiveCacheActivity(t *testing.T) {
	manager, _ := newTestSession(t)
	manager.AppendMessage(&ai.AssistantMessage{
		Provider: "anthropic", Model: "fixture", StopReason: ai.StopStop,
		Usage: ai.Usage{CacheRead: 6000000000000000000, CacheWrite: 6000000000000000000},
	})
	current := &ai.AssistantMessage{
		Provider: "anthropic", Model: "fixture", StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 10000},
	}
	for name, get := range map[string]func() (CacheMiss, bool){
		"reference": func() (CacheMiss, bool) { return DetectCacheMiss(manager.GetEntries(), current, nil) },
		"cached":    func() (CacheMiss, bool) { return manager.CacheMissFor(current, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			miss, ok := get()
			if !ok || miss.MissedTokens != 10000 {
				t.Fatalf("cache miss = %+v, counted=%v, want 10000 after reported cache activity", miss, ok)
			}
		})
	}
}

// Reference: pi v1.5.0 (f9a26365), cache-stats.ts scan adds positive
// missedTokens across entries without signed integer wraparound.
func TestCacheWasteDoesNotWrapPositiveMissTotals(t *testing.T) {
	manager, _ := newTestSession(t)
	for range 3 {
		manager.AppendMessage(&ai.AssistantMessage{
			Provider: "anthropic", Model: "fixture", StopReason: ai.StopStop,
			Usage: ai.Usage{Input: 6000000000000000000, CacheRead: 1},
		})
	}
	for name, get := range map[string]func() CacheWasteTotals{
		"reference": func() CacheWasteTotals { return ComputeCacheWaste(manager.GetEntries(), nil) },
		"cached":    func() CacheWasteTotals { return manager.ComputeCacheWaste(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			totals := get()
			if totals.MissCount != 2 || totals.MissedTokens != math.MaxInt64 {
				t.Fatalf("cache waste = %+v, want 2 misses with %d tokens", totals, int64(math.MaxInt64))
			}
		})
	}
}
