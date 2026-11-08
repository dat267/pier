package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) packages/coding-agent/src/core/session-manager.ts
// (SessionManager appends, loadEntriesFromFile, JSON.stringify per line). The
// golden records what the pinned code wrote, then what a load followed by a
// stringify rewrite produced. Pier rewrites a session after a branch edit, a
// compaction or a migration, so both must reproduce the reference's bytes
// rather than dropping a field, a line or an unknown member.
// Regenerate: node coding/testdata/session-roundtrip/generate.mjs ./pi > coding/testdata/session-roundtrip/golden.json
func TestSessionFileRoundTripsReferenceBytes(t *testing.T) {
	raw, err := os.ReadFile("testdata/session-roundtrip/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			Name    string `json:"name"`
			Seed    string `json:"seeded"`
			Content string `json:"content"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden.Cases) < 2 {
		t.Fatalf("golden holds %d cases", len(golden.Cases))
	}

	for _, testCase := range golden.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			if !strings.HasSuffix(testCase.Content, "\n") || strings.Count(testCase.Content, "\n") < 2 {
				t.Fatalf("golden holds %d bytes across %d lines", len(testCase.Content), strings.Count(testCase.Content, "\n"))
			}
			seed := testCase.Seed
			if seed == "" {
				seed = testCase.Content
			}
			dir := t.TempDir()
			source := filepath.Join(dir, "source.jsonl")
			if err := os.WriteFile(source, []byte(seed), 0o600); err != nil {
				t.Fatal(err)
			}
			entries, err := LoadEntriesFromFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != strings.Count(seed, "\n") {
				t.Fatalf("loaded %d entries, the file holds %d lines", len(entries), strings.Count(seed, "\n"))
			}
			rewritten := filepath.Join(dir, "rewritten.jsonl")
			if err := writeSessionEntries(rewritten, entries); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(rewritten)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) == testCase.Content {
				return
			}
			gotLines, wantLines := strings.Split(string(got), "\n"), strings.Split(testCase.Content, "\n")
			for index := 0; index < len(wantLines) || index < len(gotLines); index++ {
				var want, have string
				if index < len(wantLines) {
					want = wantLines[index]
				}
				if index < len(gotLines) {
					have = gotLines[index]
				}
				if want != have {
					t.Fatalf("line %d differs:\n got %s\nwant %s", index+1, have, want)
				}
			}
			t.Fatal("rewritten session differs from the reference bytes")
		})
	}

	// The entry-type case has to keep every type present, so a shrunken golden
	// cannot quietly stop covering one.
	types := map[string]bool{}
	source := filepath.Join(t.TempDir(), "types.jsonl")
	if err := os.WriteFile(source, []byte(golden.Cases[0].Content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadEntriesFromFile(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		switch {
		case entry.Header != nil:
			types["session"] = true
		case entry.Entry != nil:
			types[entry.Entry.Type] = true
		}
	}
	for _, entryType := range []string{"session", "message", "thinking_level_change", "model_change", "usage", "session_info", "custom", "custom_message", "context_edit", "label", "compaction"} {
		if !types[entryType] {
			t.Errorf("the %s case covers no %s entry", golden.Cases[0].Name, entryType)
		}
	}
}
