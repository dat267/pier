package tui

// Replays the upstream fuzzy goldens (tui/testdata/fuzzy/generate.mjs). The
// scores are compared with a tolerance because the port's ASCII fast path sums
// the same terms in a different order; the filter order must match exactly,
// since it is what the user sees in a completion list.

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestFuzzyMatchingMatchesTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/fuzzy/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var records []struct {
		Query   string `json:"query"`
		Matches []struct {
			Text    string  `json:"text"`
			Matches bool    `json:"matches"`
			Score   float64 `json:"score"`
		} `json:"matches"`
		Candidates []string `json:"candidates"`
		Order      []string `json:"order"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("golden is empty")
	}
	const tolerance = 1e-9
	for _, record := range records {
		for _, want := range record.Matches {
			got := MatchFuzzy(record.Query, want.Text)
			if got.Matches != want.Matches || math.Abs(got.Score-want.Score) > tolerance {
				t.Errorf("MatchFuzzy(%q, %q) = {%v %v}, reference gives {%v %v}",
					record.Query, want.Text, got.Matches, got.Score, want.Matches, want.Score)
			}
		}
		if record.Order == nil {
			continue
		}
		filtered := FuzzyFilter(append([]string(nil), record.Candidates...), record.Query, func(text string) string { return text })
		if len(filtered) != len(record.Order) {
			t.Fatalf("FuzzyFilter(%q) returned %d items, reference gives %d", record.Query, len(filtered), len(record.Order))
		}
		for index := range filtered {
			if filtered[index] != record.Order[index] {
				t.Errorf("FuzzyFilter(%q) item %d = %q, reference gives %q", record.Query, index, filtered[index], record.Order[index])
				break
			}
		}
	}
}
