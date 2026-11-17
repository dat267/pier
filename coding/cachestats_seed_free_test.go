package coding

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The seeding scan returns the pages it allocated once it finishes, for a session
// large enough to matter. That must not change what it installs: every message
// still contributes its facts, and the entries stay index shells (D491).
func TestCacheSeedReturnsItsPagesAfterALargeSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"session","version":3,"id":"s1","timestamp":"t0","cwd":"/tmp"}` + "\n"
	const messages = 900
	parent := ""
	for i := 0; i < messages; i++ {
		parentField := ""
		if parent != "" {
			parentField = `,"parentId":"` + parent + `"`
		}
		content += `{"type":"message","id":"m` + strconv.Itoa(i) + `","timestamp":"t"` + parentField +
			`,"message":{"role":"assistant","content":"` + strings.Repeat("x", 5000) + `"}}` + "\n"
		parent = "m" + strconv.Itoa(i)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 4<<20 {
		t.Fatalf("fixture is %d bytes, want more than the page-return threshold", info.Size())
	}

	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	<-manager.cacheSeedDone
	if got := len(manager.cacheScan.candidates); got != messages {
		t.Fatalf("candidates = %d, want %d", got, messages)
	}
	for _, fileEntry := range manager.fileEntries {
		if entry := fileEntry.Entry; entry != nil && entry.Type == "message" && !entry.lazy {
			t.Fatalf("entry %s was hydrated into the manager", entry.ID)
		}
	}
}
