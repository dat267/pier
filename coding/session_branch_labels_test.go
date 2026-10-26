package coding

import (
	"github.com/dat267/pier/ai"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts
// createBranchedSession recreates resolved bookmarks after the retained path.
func TestBranchedSessionPreservesBookmarks(t *testing.T) {
	for _, persist := range []bool{false, true} {
		name := "memory"
		if persist {
			name = "file"
		}
		t.Run(name, func(t *testing.T) {
			m := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
			user := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "keep"}, Timestamp: 1})
			label := "bookmark"
			oldLabel, err := m.AppendLabelChange(user, &label)
			if err != nil {
				t.Fatal(err)
			}
			timestamp := m.GetEntry(oldLabel).Timestamp
			leaf := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "next"}, Timestamp: 2})
			if m.GetLabel(user) != label {
				t.Fatal("control: original bookmark missing")
			}
			path, err := m.CreateBranchedSession(leaf)
			if err != nil {
				t.Fatal(err)
			}
			check := func(manager *SessionManager) {
				t.Helper()
				if got := manager.GetLabel(user); got != label {
					t.Fatalf("bookmark = %q, want %q", got, label)
				}
				entries := manager.GetEntries()
				if len(entries) != 3 {
					t.Fatalf("entries = %d, want two messages and bookmark", len(entries))
				}
				entry := entries[2]
				if entry.Type != "label" || entry.TargetID != user || entry.ID == oldLabel || entry.ParentID == nil || *entry.ParentID != leaf || entry.Timestamp != timestamp {
					t.Fatalf("recreated bookmark = %+v", entry)
				}
				if manager.GetEntry(oldLabel) != nil {
					t.Fatal("original label entry survived")
				}
			}
			check(m)
			if persist {
				reopened, err := OpenSession(path, "", "")
				if err != nil {
					t.Fatal(err)
				}
				check(reopened)
			}
		})
	}
}
