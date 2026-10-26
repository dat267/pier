package coding

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts and
// core/session-export.ts write `new Date().toISOString()`: UTC with exactly
// three fractional digits, and name files from that same string (CR-071).
var (
	jsISOTimestampPattern = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`)
	jsISOFilenamePattern  = regexp.MustCompile(`^(\d{4}-\d\d-\d\dT\d\d-\d\d-\d\d-\d{3}Z)_`)
)

func TestSessionTimestampsMatchJavaScriptISOShape(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	dir := t.TempDir()
	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: dir})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}, Timestamp: 1})
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}

	name := filepath.Base(manager.GetSessionFile())
	match := jsISOFilenamePattern.FindStringSubmatch(name)
	if match == nil {
		t.Fatalf("session file name %q does not start with a millisecond ISO timestamp", name)
	}
	if len(match[1]) != 24 {
		t.Fatalf("file timestamp %q has %d characters, want 24", match[1], len(match[1]))
	}

	data, err := os.ReadFile(manager.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		entry, err := UnmarshalFileEntry(line)
		if err != nil {
			t.Fatalf("parse %q: %v", line, err)
		}
		timestamp := ""
		if entry.Header != nil {
			timestamp = entry.Header.Timestamp
		} else if entry.Entry != nil {
			timestamp = entry.Entry.Timestamp
		}
		if !jsISOTimestampPattern.MatchString(timestamp) {
			t.Fatalf("persisted timestamp %q is not the JavaScript ISO shape", timestamp)
		}
	}
}

// A fork header and the exported header use the same shape.
func TestForkAndExportTimestampsMatchJavaScriptISOShape(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	dir := t.TempDir()
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: dir})
	leaf := source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}, Timestamp: 1})

	forked, err := ForkSessionAtEntry(source, leaf, source.GetCwd(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if header := forked.GetHeader(); header == nil || !jsISOTimestampPattern.MatchString(header.Timestamp) {
		t.Fatalf("fork header timestamp = %+v", header)
	}

	exported := SerializeSessionBranch(source, nil)
	first, _, _ := strings.Cut(exported, "\n")
	entry, err := UnmarshalFileEntry(first)
	if err != nil || entry.Header == nil {
		t.Fatalf("export header = %q (%v)", first, err)
	}
	if !jsISOTimestampPattern.MatchString(entry.Header.Timestamp) {
		t.Fatalf("export header timestamp %q is not the JavaScript ISO shape", entry.Header.Timestamp)
	}
}

// Nine-digit timestamps written by earlier builds must still load.
func TestLegacyNanosecondSessionStillLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	content := `{"type":"session","version":3,"id":"s","timestamp":"2026-01-01T00:00:00.123456789Z","cwd":"/work"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01.987654321Z","message":{"role":"user","content":"hi","timestamp":1}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(manager.Projection().Messages); got != 1 {
		t.Fatalf("messages = %d, want 1", got)
	}
	if stamp := manager.GetEntries()[0].Timestamp; stamp != "2026-01-01T00:00:01.987654321Z" {
		t.Fatalf("legacy timestamp = %q, want it preserved", stamp)
	}
}
