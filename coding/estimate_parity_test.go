package coding

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/compaction/compaction.ts estimateContextTokens.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/estimate/generate.mjs ./pi > coding/testdata/estimate/golden.json
func TestEstimateContextTokensMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/estimate/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string            `json:"name"`
		Messages []json.RawMessage `json:"messages"`
		Expected struct {
			Tokens         int  `json:"tokens"`
			UsageTokens    int  `json:"usageTokens"`
			TrailingTokens int  `json:"trailingTokens"`
			LastUsageIndex *int `json:"lastUsageIndex"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty estimate corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			messages := make([]ai.Message, 0, len(fixture.Messages))
			for _, encoded := range fixture.Messages {
				message, err := ai.UnmarshalMessage(encoded)
				if err != nil {
					t.Fatalf("decode message: %v", err)
				}
				messages = append(messages, message)
			}
			got := EstimateContextTokens(messages)
			lastUsage := got.LastUsageIndex
			if fixture.Expected.LastUsageIndex == nil {
				if lastUsage != -1 {
					t.Errorf("last usage index = %d, want none", lastUsage)
				}
			} else if lastUsage != *fixture.Expected.LastUsageIndex {
				t.Errorf("last usage index = %d, want %d", lastUsage, *fixture.Expected.LastUsageIndex)
			}
			if got.Tokens != fixture.Expected.Tokens || got.UsageTokens != fixture.Expected.UsageTokens ||
				got.TrailingTokens != fixture.Expected.TrailingTokens {
				t.Errorf("estimate = %+v, want tokens %d usage %d trailing %d", got,
					fixture.Expected.Tokens, fixture.Expected.UsageTokens, fixture.Expected.TrailingTokens)
			}
		})
	}
}
