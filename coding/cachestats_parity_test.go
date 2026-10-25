package coding

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
)

// Source-generated pi v1.4.0 (4a080252) cache-stats fixtures: the pinned
// detectCacheMiss/computeCacheWaste/collectCacheMisses run over cache-warm
// branches. Regenerate with
// node coding/testdata/cache-stats/generate.mjs ./pi > coding/testdata/cache-stats/golden.json
func TestCacheStatsCacheWarmParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/cache-stats/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	type goldenMiss struct {
		MissedTokens float64 `json:"missedTokens"`
		MissedCost   float64 `json:"missedCost"`
		IdleMs       *int64  `json:"idleMs"`
		ModelChanged bool    `json:"modelChanged"`
	}
	type goldenTotals struct {
		MissedTokens int64   `json:"missedTokens"`
		MissedCost   float64 `json:"missedCost"`
		MissCount    int     `json:"missCount"`
	}
	var fixture struct {
		Cases []struct {
			Name    string         `json:"name"`
			Entries []SessionEntry `json:"entries"`
			Miss    *goldenMiss    `json:"miss"`
			Totals  goldenTotals   `json:"totals"`
			Misses  []goldenMiss   `json:"misses"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("no fixture cases")
	}

	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			last := test.Entries[len(test.Entries)-1]
			message, err := ai.UnmarshalMessage(last.Message)
			if err != nil {
				t.Fatal(err)
			}
			request, ok := message.(*ai.AssistantMessage)
			if !ok {
				t.Fatalf("last entry is %T, want an assistant message", message)
			}

			miss, hasMiss := DetectCacheMiss(test.Entries[:len(test.Entries)-1], request, nil)
			if hasMiss != (test.Miss != nil) {
				t.Fatalf("miss present = %v, want %v", hasMiss, test.Miss != nil)
			}
			if test.Miss != nil {
				if miss.MissedTokens != int64(test.Miss.MissedTokens) {
					t.Errorf("missedTokens = %d, want %v", miss.MissedTokens, test.Miss.MissedTokens)
				}
				if miss.MissedCost != test.Miss.MissedCost {
					t.Errorf("missedCost = %v, want %v", miss.MissedCost, test.Miss.MissedCost)
				}
				if miss.ModelChanged != test.Miss.ModelChanged {
					t.Errorf("modelChanged = %v, want %v", miss.ModelChanged, test.Miss.ModelChanged)
				}
				// Upstream keeps a NaN idle gap for an unparseable entry
				// timestamp (D273), which never reaches the TTL idle label.
				if test.Miss.IdleMs == nil {
					if miss.IdleMs != 0 {
						t.Errorf("idleMs = %d, want the unknown-timestamp sentinel 0", miss.IdleMs)
					}
				} else if miss.IdleMs != *test.Miss.IdleMs {
					t.Errorf("idleMs = %d, want %d", miss.IdleMs, *test.Miss.IdleMs)
				}
			}

			totals := ComputeCacheWaste(test.Entries, nil)
			if totals.MissedTokens != test.Totals.MissedTokens ||
				totals.MissedCost != test.Totals.MissedCost ||
				totals.MissCount != test.Totals.MissCount {
				t.Errorf("totals = %+v, want %+v", totals, test.Totals)
			}
			misses := CollectCacheMisses(test.Entries, nil)
			if len(misses) != len(test.Misses) {
				t.Fatalf("misses = %d, want %d", len(misses), len(test.Misses))
			}
			for index, want := range test.Misses {
				if misses[index].Miss.MissedTokens != int64(want.MissedTokens) {
					t.Errorf("miss %d tokens = %d, want %v", index, misses[index].Miss.MissedTokens, want.MissedTokens)
				}
			}

			// The session manager's running scan state must agree with the full
			// scan for the same branch, including its cache-warm entries.
			manager := newTestSessionForProjection(t)
			for _, entry := range test.Entries[:len(test.Entries)-1] {
				switch entry.Type {
				case "message":
					decoded, err := ai.UnmarshalMessage(entry.Message)
					if err != nil {
						t.Fatal(err)
					}
					manager.AppendMessage(decoded)
				case "usage":
					manager.AppendUsage(entry.Kind, entry.Provider, entry.Model, *entry.Usage, "")
				case "compaction":
					manager.AppendCompaction("summary", "", 1000, nil, false, nil)
				}
			}
			full, fullOK := DetectCacheMiss(manager.GetEntries(), request, nil)
			running, runningOK := manager.CacheMissFor(request, nil)
			if runningOK != fullOK || running != full {
				t.Errorf("running scan %+v/%v, full scan %+v/%v", running, runningOK, full, fullOK)
			}
		})
	}
}
