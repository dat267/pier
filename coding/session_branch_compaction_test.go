package coding

import (
	"github.com/dat267/pier/ai"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts
// createBranchedSession remaps removed labels to the next retained entry.
func TestBranchedSessionKeepsCompactionWindow(t *testing.T) {
	for _, persist := range []bool{false, true} {
		for _, target := range []string{"label", "message", "self"} {
			name := "memory/"
			if persist {
				name = "file/"
			}
			t.Run(name+target, func(t *testing.T) {
				m := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
				old := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "summarized"}, Timestamp: 1})
				bookmark := "bookmark"
				label, err := m.AppendLabelChange(old, &bookmark)
				if err != nil {
					t.Fatal(err)
				}
				kept := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "keep"}, Timestamp: 2})
				first := label
				if target == "message" {
					first = kept
				} else if target == "self" {
					first = ""
				}
				compaction := m.AppendCompaction("summary", first, 10, nil, false, nil)
				wantFirst := kept
				wantMessages := 2
				if target == "self" {
					wantFirst = compaction
					wantMessages = 1
				}
				if got := len(m.Projection().Messages); got != wantMessages {
					t.Fatalf("control messages = %d, want %d", got, wantMessages)
				}
				path, err := m.CreateBranchedSession(compaction)
				if err != nil {
					t.Fatal(err)
				}
				check := func(manager *SessionManager) {
					t.Helper()
					if got := manager.GetEntry(compaction).FirstKeptEntryID; got != wantFirst {
						t.Errorf("firstKeptEntryId = %q, want %q", got, wantFirst)
					}
					messages := manager.Projection().Messages
					if len(messages) != wantMessages {
						t.Fatalf("fork projection has %d messages, want %d", len(messages), wantMessages)
					}
					if wantMessages == 2 {
						user, ok := messages[1].(*ai.UserMessage)
						if !ok || user.Content.Text != "keep" {
							t.Fatalf("retained message = %#v", messages[1])
						}
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
}
