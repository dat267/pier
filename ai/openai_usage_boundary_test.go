package ai

import (
	"math"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), api/openai-completions.ts
// parseChunkUsage subtracts cache counts and sums totals as JavaScript Numbers.
// D367 saturates results beyond the port's int64 usage representation.
func TestParseChunkUsageDoesNotWrapHugeCacheOrTotalCounts(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want Usage
	}{
		{
			name: "large total",
			raw:  `{"prompt_tokens":6000000000000000000,"completion_tokens":6000000000000000000}`,
			want: Usage{Input: 6000000000000000000, Output: 6000000000000000000, TotalTokens: math.MaxInt64},
		},
		{
			name: "large cache subtraction",
			raw:  `{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":9223372036854775807,"cache_write_tokens":9223372036854775807}}`,
			want: Usage{CacheRead: math.MaxInt64, CacheWrite: math.MaxInt64, TotalTokens: math.MaxInt64},
		},
		{
			name: "ordinary cached usage",
			raw:  `{"prompt_tokens":100,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":10}}`,
			want: Usage{Input: 70, Output: 4, CacheRead: 20, CacheWrite: 10, TotalTokens: 104},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ParseChunkUsage([]byte(test.raw), testOpenAIModel())
			if got.Input != test.want.Input || got.Output != test.want.Output || got.CacheRead != test.want.CacheRead || got.CacheWrite != test.want.CacheWrite || got.TotalTokens != test.want.TotalTokens {
				t.Fatalf("usage = %+v, want token fields %+v", got, test.want)
			}
		})
	}
}
