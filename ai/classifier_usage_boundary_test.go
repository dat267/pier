package ai

import (
	"encoding/json"
	"math"
	"testing"
)

// pi v1.5.0 api/classifier-shared.ts parseClassifierUsage keeps any finite
// positive token count in JavaScript Number space and adds the total as a
// Number. The Go port stores Usage in int64, so it must saturate instead of
// dropping values above the range or wrapping the sum.
func TestClassifierUsageSaturatesHugeFiniteCounts(t *testing.T) {
	model := &Model{Cost: ModelCost{ModelCostRates: ModelCostRates{Input: 1, Output: 2}}}
	tests := []struct {
		name          string
		raw           string
		input, output int64
		total         int64
	}{
		{name: "beyond int64", raw: `{"input_tokens":1e308,"output_tokens":1e308}`,
			input: math.MaxInt64, output: math.MaxInt64, total: math.MaxInt64},
		{name: "sum overflow", raw: `{"input_tokens":6e18,"output_tokens":6e18}`,
			input: 6_000_000_000_000_000_000, output: 6_000_000_000_000_000_000, total: math.MaxInt64},
	}
	parsers := []struct {
		label string
		run   func(json.RawMessage, *Model) *Usage
	}{
		{label: "openai-decisions", run: parseOpenAIDecisionsUsage},
		{label: "system-one", run: parseSystemOneUsage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, parser := range parsers {
				usage := parser.run(json.RawMessage(test.raw), model)
				if usage == nil {
					t.Fatalf("%s usage was not parsed", parser.label)
				}
				if usage.Input != test.input || usage.Output != test.output || usage.TotalTokens != test.total {
					t.Errorf("%s usage = input %d output %d total %d, want %d %d %d",
						parser.label, usage.Input, usage.Output, usage.TotalTokens, test.input, test.output, test.total)
				}
			}
		})
	}
}
