package coding

import (
	"github.com/dat267/pier/ai"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts
// createBranchedSession captures previousSessionFile before replacing it.
func TestBranchedSessionRecordsItsSourceFile(t *testing.T) {
	for _, persist := range []bool{false, true} {
		name := "memory"
		if persist {
			name = "file"
		}
		t.Run(name, func(t *testing.T) {
			m := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
			id := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "keep"}, Timestamp: 1})
			source := m.GetSessionFile()
			path, err := m.CreateBranchedSession(id)
			if err != nil {
				t.Fatal(err)
			}
			check := func(manager *SessionManager) {
				t.Helper()
				h := manager.GetHeader()
				if !persist {
					if h.ParentSession != nil {
						t.Fatalf("memory branch has parent %q", *h.ParentSession)
					}
					return
				}
				if h.ParentSession == nil {
					t.Fatal("fork has no parentSession")
				}
				if *h.ParentSession != source {
					t.Fatalf("parentSession = %q, want source %q", *h.ParentSession, source)
				}
			}
			check(m)
			if persist {
				if path == source {
					t.Fatal("fork overwrote source")
				}
				reopened, err := OpenSession(path, "", "")
				if err != nil {
					t.Fatal(err)
				}
				check(reopened)
			}
		})
	}
}
