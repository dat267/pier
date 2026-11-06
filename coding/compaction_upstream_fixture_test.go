package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), core/compaction/compaction.ts findCutPoint
// and prepareCompaction over the upstream v1 fixture
// test/fixtures/before-compaction.jsonl. Regenerate the golden with
// `node --experimental-strip-types coding/testdata/upstream-compaction/generate.mjs ./pi`.
//
// The fixture predates the v2 tree fields, so this also pins the v1 migration:
// the expected entry count, the cut index, the split turn and the previous
// summary only line up when the migrated entries and the projection agree with
// the reference.
func TestPrepareCompactionMatchesUpstreamFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/upstream-compaction/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Entries  int `json:"entries"`
		Settings struct {
			Enabled          bool  `json:"enabled"`
			ReserveTokens    int64 `json:"reserveTokens"`
			KeepRecentTokens int64 `json:"keepRecentTokens"`
		} `json:"settings"`
		Cut struct {
			Index       int    `json:"index"`
			TurnStart   int    `json:"turnStartIndex"`
			IsSplitTurn bool   `json:"isSplitTurn"`
			Type        string `json:"type"`
		} `json:"cut"`
		Preparation struct {
			FirstKeptIndex        int   `json:"firstKeptIndex"`
			TokensBefore          int64 `json:"tokensBefore"`
			IsSplitTurn           bool  `json:"isSplitTurn"`
			Summarize             int   `json:"summarize"`
			TurnPrefix            int   `json:"turnPrefix"`
			PreviousSummaryLength int   `json:"previousSummaryLength"`
		} `json:"preparation"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}

	fixture, err := os.ReadFile(filepath.Join("testdata", "upstream-compaction", "before-compaction.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "fixture.jsonl")
	if err := os.WriteFile(copyPath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(copyPath, "", "")
	if err != nil {
		t.Fatalf("open migrated fixture: %v", err)
	}
	entries := manager.GetEntries()
	if len(entries) != golden.Entries {
		t.Fatalf("entries = %d, want %d", len(entries), golden.Entries)
	}

	settings := DefaultCompactionSettings
	settings.Enabled = golden.Settings.Enabled
	settings.ReserveTokens = golden.Settings.ReserveTokens
	settings.KeepRecentTokens = golden.Settings.KeepRecentTokens
	preparation := PrepareCompaction(entries, settings)
	if preparation == nil {
		t.Fatal("no preparation for the upstream fixture")
	}

	cutIndex := -1
	for index := range entries {
		if entries[index].ID == preparation.FirstKeptEntryID {
			cutIndex = index
		}
	}
	if cutIndex != golden.Cut.Index || cutIndex != golden.Preparation.FirstKeptIndex {
		t.Fatalf("first kept index = %d, want %d/%d", cutIndex, golden.Cut.Index, golden.Preparation.FirstKeptIndex)
	}
	if entries[cutIndex].Type != golden.Cut.Type {
		t.Errorf("first kept type = %s, want %s", entries[cutIndex].Type, golden.Cut.Type)
	}
	if preparation.IsSplitTurn != golden.Cut.IsSplitTurn || preparation.IsSplitTurn != golden.Preparation.IsSplitTurn {
		t.Errorf("isSplitTurn = %v, want %v", preparation.IsSplitTurn, golden.Cut.IsSplitTurn)
	}
	if int64(preparation.TokensBefore) != golden.Preparation.TokensBefore {
		t.Errorf("tokensBefore = %d, want %d", preparation.TokensBefore, golden.Preparation.TokensBefore)
	}
	if len(preparation.MessagesToSummarize) != golden.Preparation.Summarize {
		t.Errorf("summarize = %d, want %d", len(preparation.MessagesToSummarize), golden.Preparation.Summarize)
	}
	if len(preparation.TurnPrefixMessages) != golden.Preparation.TurnPrefix {
		t.Errorf("turn prefix = %d, want %d", len(preparation.TurnPrefixMessages), golden.Preparation.TurnPrefix)
	}
	if JSLength(preparation.PreviousSummary) != golden.Preparation.PreviousSummaryLength {
		t.Errorf("previous summary length = %d, want %d", JSLength(preparation.PreviousSummary), golden.Preparation.PreviousSummaryLength)
	}
}
