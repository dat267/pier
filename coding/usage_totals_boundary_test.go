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

// Cache-warming usage is recorded as a `usage` entry carrying its own provider
// and model, and upstream's breakdown attributes it to `provider/model` and its
// tokens to the session totals (usage-totals.ts checks `entry.type === "usage"`,
// getSessionStats adds it to the running totals). The port dropped both.
func TestUsageEntriesAreAttributed(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
	manager.AppendMessage(&ai.AssistantMessage{
		Provider: "anthropic", Model: "sonnet", Content: ai.ContentList{ai.TextContent{Text: "a"}},
		Usage: ai.Usage{Input: 10, Cost: ai.UsageCost{Total: 0.1}}, StopReason: ai.StopStop,
	})
	manager.AppendUsage("cache_warm", "anthropic", "sonnet-4",
		ai.Usage{Input: 100, Output: 1, CacheRead: 5, CacheWrite: 2, Cost: ai.UsageCost{Total: 0.25}}, "")

	want := []UsageCostBreakdownEntry{
		{Key: "anthropic/sonnet-4", Cost: 0.25, Tokens: 108},
		{Key: "anthropic/sonnet", Cost: 0.1, Tokens: 10},
	}
	got := manager.UsageCostBreakdown()
	if len(got) != len(want) {
		t.Fatalf("breakdown = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if reference := GetUsageCostBreakdown(manager.GetEntries()); len(reference) != len(want) {
		t.Errorf("the reference walk dropped the usage entry too: %+v", reference)
	}

	// The usage entry counts in the totals, but it is not a message.
	stats := manager.SessionStats()
	if stats.TotalMessages != 1 || stats.AssistantMessages != 1 {
		t.Errorf("counts = %+v", stats)
	}
	if stats.Tokens.Input != 110 || stats.Tokens.CacheRead != 5 || stats.Tokens.Total != 118 {
		t.Errorf("tokens = %+v", stats.Tokens)
	}
	if stats.Cost != 0.35 {
		t.Errorf("cost = %v", stats.Cost)
	}
}

// D555: the reference's filter and sort compare `entry.cost` > 0 and
// `b.cost - a.cost` where `cost` is the UsageCost OBJECT (usage-totals.ts:57-58
// declares the field as number but :93 assigns `totals.cost`, the object). An
// object compared to a number is NaN, so upstream's filter keeps only entries
// with positive TOKENS and its sort is a no-op. The port compares the numeric
// Total and sorts descending, which is what both lines intend.
func TestUsageCostBreakdownFiltersAndSortsByNumericCost(t *testing.T) {
	totalsByKey := map[string]UsageTotals{
		// Cost only, no tokens: upstream's filter drops this because
		// object > 0 is false; the port keeps it.
		"costly": {Cost: 5},
		// Real work, lower cost: must sort BELOW "costly".
		"cheap": {Cost: 1, Input: 10},
	}
	got := rankUsageBreakdown([]string{"cheap", "costly"}, totalsByKey)
	if len(got) != 2 {
		t.Fatalf("entries = %+v, want both cost-bearing buckets", got)
	}
	if got[0].Key != "costly" || got[1].Key != "cheap" {
		t.Fatalf("order = [%s %s], want costly before cheap (descending cost)", got[0].Key, got[1].Key)
	}
	// Ties keep insertion order, which is what a stable sort gives and what
	// Array.prototype.sort would give if it compared numbers.
	same := rankUsageBreakdown([]string{"a", "b", "c"}, map[string]UsageTotals{
		"a": {Cost: 2}, "b": {Cost: 2}, "c": {Cost: 2},
	})
	var order []string
	for _, entry := range same {
		order = append(order, entry.Key)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("tie order = %v, want insertion order", order)
	}
}
