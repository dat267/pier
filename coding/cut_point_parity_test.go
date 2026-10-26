package coding

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), core/compaction/compaction.ts findCutPoint.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/cut-point/generate.mjs ./pi > coding/testdata/cut-point/golden.json
func TestFindCutPointMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/cut-point/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []struct {
		Name    string            `json:"name"`
		Entries []json.RawMessage `json:"entries"`
		Cases   []struct {
			StartIndex       int `json:"startIndex"`
			EndIndex         int `json:"endIndex"`
			KeepRecentTokens int `json:"keepRecentTokens"`
			Expected         struct {
				FirstKeptEntryIndex int  `json:"firstKeptEntryIndex"`
				TurnStartIndex      int  `json:"turnStartIndex"`
				IsSplitTurn         bool `json:"isSplitTurn"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &scenarios); err != nil {
		t.Fatal(err)
	}
	if len(scenarios) == 0 {
		t.Fatal("empty cut-point corpus")
	}
	for _, scenario := range scenarios {
		entries := make([]SessionEntry, 0, len(scenario.Entries))
		for _, encoded := range scenario.Entries {
			fileEntry, err := UnmarshalFileEntry(string(encoded))
			if err != nil {
				t.Fatalf("%s: decode entry: %v", scenario.Name, err)
			}
			if fileEntry.Entry == nil {
				t.Fatalf("%s: entry has no body", scenario.Name)
			}
			entries = append(entries, *fileEntry.Entry)
		}
		for _, testCase := range scenario.Cases {
			got := FindCutPoint(entries, testCase.StartIndex, testCase.EndIndex, testCase.KeepRecentTokens)
			if got.FirstKeptEntryIndex != testCase.Expected.FirstKeptEntryIndex ||
				got.TurnStartIndex != testCase.Expected.TurnStartIndex || got.IsSplitTurn != testCase.Expected.IsSplitTurn {
				t.Errorf("%s start=%d end=%d keep=%d: cut = %+v, want %+v", scenario.Name,
					testCase.StartIndex, testCase.EndIndex, testCase.KeepRecentTokens, got, testCase.Expected)
			}
		}
	}
}
