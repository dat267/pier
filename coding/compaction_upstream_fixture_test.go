package coding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
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
		Projection struct {
			Count  int    `json:"count"`
			Digest string `json:"digest"`
		} `json:"projection"`
		Conversation struct {
			Length int    `json:"length"`
			Digest string `json:"digest"`
		} `json:"conversation"`
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

	if got, want := projectionDigest(t, manager.Projection().Messages), golden.Projection.Digest; got != want {
		t.Errorf("projection digest = %s, want %s", got, want)
	}
	if got := len(manager.Projection().Messages); got != golden.Projection.Count {
		t.Errorf("projection messages = %d, want %d", got, golden.Projection.Count)
	}

	serialized := SerializeConversation(ConvertToLlm(manager.Projection().Messages))
	if JSLength(serialized) != golden.Conversation.Length {
		t.Errorf("serialized conversation length = %d, want %d", JSLength(serialized), golden.Conversation.Length)
	}
	if got := sha256Hex(serialized); got != golden.Conversation.Digest {
		t.Errorf("serialized conversation digest = %s, want %s", got, golden.Conversation.Digest)
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

// projectionDigest describes each projected message the way the golden does:
// the role, then one entry per content block with the JS length of its text,
// thinking or tool arguments. Only lengths are compared, so key order and
// escaping differences cannot mask a content change.
func projectionDigest(t *testing.T, messages []ai.Message) string {
	t.Helper()
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		var blocks []string
		switch m := message.(type) {
		case *ai.UserMessage:
			if m.Content.String() {
				blocks = append(blocks, "text:"+strconv.Itoa(JSLength(m.Content.Text)))
			} else {
				blocks = append(blocks, describeProjectedBlocks(m.Content.Blocks)...)
			}
		case *ai.AssistantMessage:
			blocks = append(blocks, describeProjectedBlocks(m.Content)...)
		case *ai.ToolResultMessage:
			blocks = append(blocks, describeProjectedBlocks(m.Content)...)
		}
		lines = append(lines, string(ai.RoleOf(message))+"|"+strings.Join(blocks, ","))
	}
	return sha256Hex(strings.Join(lines, "\n"))
}

func describeProjectedBlocks(blocks any) []string {
	described := make([]string, 0, 4)
	switch list := blocks.(type) {
	case ai.ContentList:
		for _, block := range list {
			described = append(described, describeProjectedBlock(block))
		}
	case ai.UserContentList:
		for _, block := range list {
			described = append(described, describeProjectedBlock(block))
		}
	}
	return described
}

func describeProjectedBlock(block any) string {
	switch b := block.(type) {
	case ai.TextContent:
		return "text:" + strconv.Itoa(JSLength(b.Text))
	case ai.ThinkingContent:
		return "thinking:" + strconv.Itoa(JSLength(b.Thinking))
	case ai.ToolCall:
		encoded, _ := ai.MarshalJSON(b.Arguments)
		return "toolCall:" + b.Name + ":" + strconv.Itoa(JSLength(string(encoded)))
	case ai.ImageContent:
		return "image"
	default:
		return "other"
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
