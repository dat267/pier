package ai

// Port of providers/openrouter-api.ts openRouterCost: OpenRouter prices arrive
// as per-token decimal strings and become pi's per-million rates.

import (
	"encoding/json"
	"testing"
)

func rawPrice(text string) json.RawMessage {
	raw, _ := json.Marshal(text)
	return raw
}

func TestOpenRouterPerMillionFollowsParseFloat(t *testing.T) {
	cases := []struct {
		raw      json.RawMessage
		fallback float64
		want     float64
	}{
		{rawPrice("0.000003"), 0, 3},
		{rawPrice("0.000001"), 0, 1},
		{rawPrice("0.0000005"), 0, 0.5},
		{rawPrice("1e-6"), 0, 1},
		// parseFloat stops at the first invalid character and keeps the prefix.
		{rawPrice("0.5abc"), 0, 500000},
		{rawPrice("1,5"), 0, 1_000_000},
		// Leading and trailing whitespace is allowed.
		{rawPrice("  0.25  "), 0, 250000},
		// Negative prices (OpenRouter's router models report -1) and invalid
		// text keep the fallback.
		{rawPrice("-1"), 7, 7},
		{rawPrice("abc"), 7, 7},
		{rawPrice(""), 7, 7},
		{nil, 7, 7},
		{json.RawMessage("null"), 7, 7},
		// A JSON number is accepted as well.
		{json.RawMessage("0.000003"), 0, 3},
	}
	for _, test := range cases {
		if got := openRouterPerMillion(test.raw, test.fallback); got != test.want {
			t.Errorf("perMillion(%s, %v) = %v, want %v", test.raw, test.fallback, got, test.want)
		}
	}
}

// D485: the reference lets parseFloat overflow to Infinity, which JSON cannot
// represent and which would poison every later cost sum; the port keeps the
// fallback instead.
func TestOpenRouterPriceOverflowKeepsTheFallback(t *testing.T) {
	for _, raw := range []json.RawMessage{rawPrice("1e999"), rawPrice("Infinity"), rawPrice("-1e999")} {
		if got := openRouterPerMillion(raw, 4); got != 4 {
			t.Errorf("perMillion(%s, 4) = %v, want the fallback 4", raw, got)
		}
	}
}

func TestOpenRouterModelCostBuildsPromptTiers(t *testing.T) {
	pricing := openRouterPricing{
		Prompt:     rawPrice("0.000001"),
		Completion: rawPrice("0.000002"),
		Overrides: []openRouterPricingOverride{
			{MinPromptTokens: int64Ptr(1000), Prompt: rawPrice("0.000002")},
			// No threshold: dropped.
			{Prompt: rawPrice("0.000003")},
			// Time-based pricing is skipped, since ModelCost cannot express it.
			{MinPromptTokens: int64Ptr(2000), UTCStart: rawPrice("0")},
			// A missing rate keeps the base rate.
			{MinPromptTokens: int64Ptr(0), Completion: rawPrice("0.000004")},
		},
	}
	cost := openRouterModelCost(pricing)
	if cost.Input != 1 || cost.Output != 2 || cost.CacheRead != 0 || cost.CacheWrite != 0 {
		t.Fatalf("base = %+v", cost.ModelCostRates)
	}
	if len(cost.Tiers) != 2 {
		t.Fatalf("tiers = %+v", cost.Tiers)
	}
	if cost.Tiers[0].InputTokensAbove != 1000 || cost.Tiers[0].Input != 2 || cost.Tiers[0].Output != 2 {
		t.Fatalf("first tier = %+v", cost.Tiers[0])
	}
	if cost.Tiers[1].InputTokensAbove != 0 || cost.Tiers[1].Input != 1 || cost.Tiers[1].Output != 4 {
		t.Fatalf("second tier = %+v", cost.Tiers[1])
	}
}

func int64Ptr(value int64) *int64 { return &value }
