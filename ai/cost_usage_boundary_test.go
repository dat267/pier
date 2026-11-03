package ai

import "testing"

// pi v1.5.0 models.ts calculateCost compares the Number sum of input,
// cacheRead and cacheWrite before selecting a cost tier. The Go port must not
// wrap that sum in int64 before the comparison.
func TestCalculateCostSelectsTierFromNumberSpaceInputTotal(t *testing.T) {
	model := &Model{Cost: ModelCost{
		ModelCostRates: ModelCostRates{Input: 3},
		Tiers:          []ModelCostTier{{Input: 2, InputTokensAbove: 100_000}},
	}}
	usage := Usage{Input: 6e18, CacheRead: 6e18, CacheWrite: 6e18}
	cost := CalculateCost(model, &usage)
	want := (2.0 / 1_000_000.0) * float64(usage.Input)
	if cost.Input != want {
		t.Fatalf("tiered input cost = %v, want %v", cost.Input, want)
	}
}
