package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 core/session-manager.ts loadEntriesFromFile returns no
// entries unless the first parsed entry is a session header with a string id,
// so SessionManager._setSessionFile rejects a non-session file instead of
// loading its entries.
func TestOpenSessionRejectsAFileWithoutASessionHeader(t *testing.T) {
	message := `{"type":"message","id":"a","parentId":null,"timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"hi","timestamp":1}}` + "\n"
	for name, content := range map[string]string{
		"message only":        message,
		"junk before message": "garbage\n" + message,
		"non-session header":  `{"type":"compaction","id":"c","timestamp":"t"}` + "\n" + message,
	} {
		path := filepath.Join(t.TempDir(), "session.jsonl")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenSession(path, "", ""); err == nil || !strings.Contains(err.Error(), "not a valid pi session") {
			t.Errorf("%s: OpenSession error = %v; want a not-a-valid-pi-session error", name, err)
		}
	}
}

// An empty file is a fresh session, as upstream's zeroentry path rewrites it.
func TestOpenSessionInitializesAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSession(path, "", ""); err != nil {
		t.Fatalf("OpenSession(empty) error = %v", err)
	}
}
