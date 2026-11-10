package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) reads a session line with JSON.parse, which
// accepts an unpaired surrogate escape and re-emits it from JSON.stringify.
// encoding/json/v2 rejects one at the lexer (A486), so a line the reference
// keeps was dropped here, and a v1/v2-to-v3 migration then rewrote the file
// without it. Relaxing the decode cannot change input that already decoded.
const (
	surrogateSessionKeep = `{"type":"custom","id":"keep","parentId":null,"timestamp":"t","customType":"x","data":{"a":1}}`
	surrogateSessionDrop = `{"type":"custom","id":"surr","parentId":"keep","timestamp":"t","customType":"x","data":"\ud83d"}`
)

func writeSurrogateSession(t *testing.T, dir, version string, lines ...string) string {
	t.Helper()
	header := `{"type":"session","version":` + version + `,"id":"h","timestamp":"2026-01-01T00:00:00.000Z","cwd":"` + dir + `"}`
	path := filepath.Join(dir, "s.jsonl")
	body := header + "\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A lone-surrogate entry survives a load and the migration's rewrite of the file.
func TestLoneSurrogateEntrySurvivesMigration(t *testing.T) {
	dir := t.TempDir()
	path := writeSurrogateSession(t, dir, "2", surrogateSessionKeep, surrogateSessionDrop)

	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if got := len(manager.GetBranch("")); got != 2 {
		t.Errorf("loaded %d entries, want 2: the lone-surrogate entry was dropped", got)
	}
	// The migration rewrites the file; the entry must still be there.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"id":"surr"`) {
		t.Errorf("the migration deleted the lone-surrogate entry from the session file:\n%s", after)
	}
}

// The message reaches the transcript, not just the entry list.
func TestLoneSurrogateMessageReachesProjection(t *testing.T) {
	dir := t.TempDir()
	good := `{"type":"message","id":"m1","parentId":null,"timestamp":"t","message":{"role":"user","content":[{"type":"text","text":"ok"}]}}`
	bad := `{"type":"message","id":"m2","parentId":"m1","timestamp":"t","message":{"role":"user","content":[{"type":"text","text":"\ud83d"}]}}`
	path := writeSurrogateSession(t, dir, "3", good, bad)

	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if got := len(manager.GetBranch("")); got != 2 {
		t.Fatalf("loaded %d entries, want 2", got)
	}
	if got := len(manager.Projection().Messages); got != 2 {
		t.Errorf("projected %d messages, want 2: the lone-surrogate message was dropped", got)
	}
}

// A duplicate member name is also valid for JSON.parse (last value wins), and
// json/v2 rejects it by default; the fast scanner keeps the line, so the strict
// decoder must not drop it either.
func TestDuplicateMemberNameEntrySurvivesLoad(t *testing.T) {
	dir := t.TempDir()
	dup := `{"type":"custom","id":"dup","parentId":null,"timestamp":"t","customType":"x","data":{"a":1,"a":2}}`
	path := writeSurrogateSession(t, dir, "3", dup)

	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if got := len(manager.GetBranch("")); got != 1 {
		t.Errorf("loaded %d entries, want 1: the duplicate-member entry was dropped", got)
	}
}
