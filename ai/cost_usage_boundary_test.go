package ai

import (
	"testing"
)

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

// Reference: pi v1.5.2 (b4afdddf), api/simple-options.ts
// clampMaxTokensToContext subtracts the usage and the safety margin, which is
// the larger of 4096 tokens and 15% of the counted input, as Numbers.
// D313 saturates an oversized usage estimate at Go's int bound, but reserving
// the margin must not wrap it back into positive answer room.
func TestClampMaxTokensToContextKeepsHugeUsageOutOfAnswerRoom(t *testing.T) {
	context := TranscriptContext{Messages: []Message{
		&AssistantMessage{
			Usage:      Usage{TotalTokens: int64(^uint64(0) >> 1)},
			StopReason: StopStop,
		},
	}}
	for _, window := range []int64{2048, 128000} {
		model := &Model{ContextWindow: window}
		if got := ClampMaxTokensToContext(model, context, 1024); got != 1 {
			t.Errorf("window %d: max tokens = %d, want 1 for an overfull context", window, got)
		}
	}
	model := &Model{ContextWindow: 8192}
	if got := ClampMaxTokensToContext(model, TranscriptContext{}, 1024); got != 1024 {
		t.Errorf("empty context: max tokens = %d, want 1024", got)
	}
}
