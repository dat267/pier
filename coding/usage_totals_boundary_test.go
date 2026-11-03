package coding

import (
	"math"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.0 (f9a26365), coding-agent/src/core/agent-session.ts
// getSessionStats and usage-totals.ts sum positive counters as Numbers.
func TestSessionStatsDoNotWrapPositiveUsage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		usage      ai.Usage
		count      int
		wantColumn int64
		wantTotal  int64
	}{
		{name: "cross-field sum", usage: ai.Usage{Input: 6000000000000000000, Output: 6000000000000000000, CacheRead: 6000000000000000000, CacheWrite: 6000000000000000000}, count: 1, wantColumn: 6000000000000000000, wantTotal: math.MaxInt64},
		{name: "column accumulation", usage: ai.Usage{Input: 6000000000000000000, Output: 6000000000000000000, CacheRead: 6000000000000000000, CacheWrite: 6000000000000000000}, count: 2, wantColumn: math.MaxInt64, wantTotal: math.MaxInt64},
		{name: "ordinary", usage: ai.Usage{Input: 10, Output: 10, CacheRead: 10, CacheWrite: 10}, count: 2, wantColumn: 20, wantTotal: 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, _ := newTestSession(t)
			for range tc.count {
				manager.AppendMessage(&ai.AssistantMessage{Provider: "anthropic", Model: "fixture", Usage: tc.usage, StopReason: ai.StopStop})
			}
			stats := manager.SessionStats()
			for name, count := range map[string]int64{"input": stats.Tokens.Input, "output": stats.Tokens.Output, "cacheRead": stats.Tokens.CacheRead, "cacheWrite": stats.Tokens.CacheWrite} {
				if count != tc.wantColumn {
					t.Errorf("%s count = %d, want %d", name, count, tc.wantColumn)
				}
			}
			if stats.Tokens.Total != tc.wantTotal {
				t.Errorf("total = %d, want %d", stats.Tokens.Total, tc.wantTotal)
			}
		})
	}
}

// Reference: pi v1.5.0 (f9a26365), coding-agent/src/core/usage-totals.ts
// getUsageCostBreakdown keeps positive-token buckets even when their cost is 0.
func TestUsageCostBreakdownDoesNotDropHugeTokenBuckets(t *testing.T) {
	manager, _ := newTestSession(t)
	manager.AppendMessage(&ai.AssistantMessage{
		Provider: "anthropic", Model: "fixture", StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 6000000000000000000, Output: 6000000000000000000},
	})
	for name, get := range map[string]func() []UsageCostBreakdownEntry{
		"reference": func() []UsageCostBreakdownEntry { return GetUsageCostBreakdown(manager.GetEntries()) },
		"cached":    manager.UsageCostBreakdown,
	} {
		t.Run(name, func(t *testing.T) {
			buckets := get()
			if len(buckets) != 1 {
				t.Fatalf("model buckets = %v, want one positive-token bucket", buckets)
			}
			if buckets[0].Key != "anthropic/fixture" || buckets[0].Tokens != math.MaxInt64 {
				t.Fatalf("model bucket = %+v, want anthropic/fixture with %d tokens", buckets[0], int64(math.MaxInt64))
			}
		})
	}
}
