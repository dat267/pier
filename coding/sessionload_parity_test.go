package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type sessionLoadEntry struct {
	Kind string `json:"kind"`
	Type string `json:"type"`
	ID   any    `json:"id"`
}

// TestSessionLoadMatchesUpstream pins loadEntriesFromFile's header validation:
// a session header needs a string id (an empty string counts) and is rejected
// otherwise, so listing and loading agree. Regenerate with
// node coding/testdata/session-load/generate.mjs ./pi > coding/testdata/session-load/golden.json
func TestSessionLoadMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/session-load/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]struct {
		Lines   []string           `json:"lines"`
		Entries []sessionLoadEntry `json:"entries"`
		List    *struct {
			Count int   `json:"count"`
			IDs   []any `json:"ids"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture) == 0 {
		t.Fatal("fixture is empty")
	}
	for name, test := range fixture {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			content := strings.Join(test.Lines, "\n") + "\n"
			file := filepath.Join(dir, "session.jsonl")
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			entries, err := LoadEntriesFromFile(file)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]sessionLoadEntry, 0, len(entries))
			for _, entry := range entries {
				switch {
				case entry.Header != nil:
					got = append(got, sessionLoadEntry{Kind: "session", Type: "session", ID: entry.Header.ID})
				case entry.Entry != nil:
					got = append(got, sessionLoadEntry{Kind: "entry", Type: entry.Entry.Type, ID: entry.Entry.ID})
				}
			}
			if !reflect.DeepEqual(got, test.Entries) {
				t.Errorf("entries = %+v, want %+v", got, test.Entries)
			}
			if test.List == nil {
				return
			}
			sessions := ListSessions("/work", dir)
			ids := make([]any, 0, len(sessions))
			for _, session := range sessions {
				ids = append(ids, session.ID)
			}
			if len(sessions) != test.List.Count || !reflect.DeepEqual(ids, test.List.IDs) {
				t.Errorf("listed = %d %v, want %d %v", len(sessions), ids, test.List.Count, test.List.IDs)
			}
		})
	}
}
