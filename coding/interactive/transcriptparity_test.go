package interactive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// TestTranscriptCompositionUpstreamParity pins the entry-to-message mapping and
// the compaction-aware entry list a transcript renders against pi v1.5.2
// (b4afdddf), core/session-manager.ts sessionEntryToContextMessages and
// buildContextEntries. Regenerate from the repository root:
//
//	node --experimental-strip-types coding/interactive/testdata/gen-transcript-parity.mjs ./pi > coding/interactive/testdata/transcript_parity_golden.txt
//
// It covers every message role with null, missing and block content, custom
// messages with and without display, a branch summary with and without a
// summary, a compaction with and without its recorded system message, entries
// that carry no message, and the leaf path through a linear chain, a fork, a
// compaction and a dangling parent id.
func TestTranscriptCompositionUpstreamParity(t *testing.T) {
	var corpus struct {
		Cases map[string]struct {
			Entries  []json.RawMessage `json:"entries"`
			LeafID   *string           `json:"leafId"`
			Messages []string          `json:"messages"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/transcript_corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}

	golden := loadGoldenFile(t, "testdata/transcript_parity_golden.txt")

	for _, label := range sortedKeys(corpus.Cases) {
		spec := corpus.Cases[label]
		entries := make([]coding.SessionEntry, 0, len(spec.Entries))
		byID := make(map[string]*coding.SessionEntry, len(spec.Entries))
		for _, raw := range spec.Entries {
			var entry coding.SessionEntry
			if err := json.Unmarshal(raw, &entry); err != nil {
				t.Fatalf("%s: parse entry: %v", label, err)
			}
			entries = append(entries, entry)
		}
		for i := range entries {
			byID[entries[i].ID] = &entries[i]
		}

		messages := map[string]any{}
		for _, id := range spec.Messages {
			entry, ok := byID[id]
			if !ok {
				t.Fatalf("%s: no entry %q", label, id)
			}
			converted := coding.SessionEntryToContextMessages(entry)
			if converted == nil {
				// The reference returns an empty array where the port returns nil;
				// callers only read the length, so compare them as the same thing.
				converted = []ai.Message{}
			}
			encoded, err := ai.MarshalMessages(converted)
			if err != nil {
				t.Fatalf("%s: marshal %q: %v", label, id, err)
			}
			messages[id] = encoded
		}
		checkGolden(t, golden, label+"-messages", messages)

		context := coding.BuildContextEntries(entries, spec.LeafID, byID)
		ids := make([]string, 0, len(context))
		for _, entry := range context {
			ids = append(ids, entry.ID)
		}
		checkGolden(t, golden, label+"-context", ids)
	}
}
