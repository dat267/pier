package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// TestSerializeSessionBranchHeader guards D130: the serialized branch starts
// with a valid session header (`type: "session"`).
func TestSerializeSessionBranchHeader(t *testing.T) {
	manager := NewSessionManager("/tmp/proj", &SessionManagerOptions{Persist: boolPtr(false)})
	manager.AppendMessage(ai.Message(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}))
	content := SerializeSessionBranch(manager, func(parentID *string, timestamp string) []*SessionEntry {
		entry := &SessionEntry{Type: "custom", CustomType: "pi.share"}
		entry.ID = "trailing1"
		entry.ParentID = parentID
		entry.Timestamp = timestamp
		entry.Data = json.RawMessage(`{"systemPrompt":"p"}`)
		return []*SessionEntry{entry}
	})
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d: %q", len(lines), content)
	}
	if !strings.Contains(lines[0], `"type":"session"`) {
		t.Fatalf("header = %s", lines[0])
	}
	if !strings.Contains(lines[2], `"pi.share"`) || !strings.Contains(lines[2], `"data":{"systemPrompt":"p"}`) {
		t.Fatalf("trailing = %s", lines[2])
	}
	// The content parses as a session file.
	path := filepath.Join(t.TempDir(), "branch.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	opened, err := OpenSession(path, "", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(opened.GetEntries()) != 2 {
		t.Fatalf("entries = %d", len(opened.GetEntries()))
	}
}

// Reference: pi v1.3.0 core/session-export.ts serializes before writeFileSync;
// a serialization failure cannot truncate an existing destination.
func TestExportSessionSerializationFailurePreservesDestination(t *testing.T) {
	for _, trailing := range []bool{false, true} {
		name := "branch"
		if trailing {
			name = "trailing"
		}
		t.Run(name, func(t *testing.T) {
			manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: boolPtr(false)})
			manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
			var create func(*string, string) []*SessionEntry
			if trailing {
				create = func(*string, string) []*SessionEntry {
					return []*SessionEntry{{Type: "custom", CustomType: "broken", Data: json.RawMessage(`{`)}}
				}
			} else {
				manager.AppendCustomEntry("broken", json.RawMessage(`{`))
			}
			path := filepath.Join(t.TempDir(), "keep.jsonl")
			const before = "existing export\n"
			if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ExportSessionToJsonl(manager, path, create); err == nil {
				t.Fatal("invalid serialization reported success")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != before {
				t.Fatalf("destination=%q err=%v", data, err)
			}
		})
	}
}
