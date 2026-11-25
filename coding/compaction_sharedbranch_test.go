package coding

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// openIndexOnlySession writes a small session and opens it so its entries are
// index-only shells (D488), which is what a real load produces.
func openIndexOnlySession(t *testing.T) *SessionManager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := []string{`{"type":"session","version":3,"id":"s1","timestamp":"t0","cwd":"/tmp"}`}
	parent := ""
	for i := range 3 {
		id := "m" + itoa(i)
		parentField := ""
		if parent != "" {
			parentField = `,"parentId":"` + parent + `"`
		}
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		lines = append(lines, `{"type":"message","id":"`+id+`","timestamp":"t"`+parentField+
			`,"message":{"role":"`+role+`","content":"body `+itoa(i)+`"}}`)
		parent = id
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	manager.waitForCacheSeed()
	return manager
}

func itoa(i int) string { return string(rune('0' + i)) }

// GetBranch("") hands out the SHARED branch cache slice, and
// prepareCompactionWith hydrates the entries it is given in place (lazy, raw,
// Message). Two concurrent compactions on one session therefore wrote the same
// SessionEntry structs. The CI race gate reported exactly that: a Write in
// payloadReader.fill against a Read in hydrateEntriesSeq, reached through two
// interleaved rpcRuntime "compact" commands
// (TestRPCAnswersEveryCommandOnceUnderAnInterleavedLoad).
func TestConcurrentCompactionsDoNotShareEntryStructs(t *testing.T) {
	manager := openIndexOnlySession(t)
	settings := CompactionSettings{Enabled: true}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { _ = manager.PrepareCompaction(settings) })
	}
	wg.Wait()
}

// The cache must not observe the hydration: a compaction preparation has to
// hydrate a copy, so the shared shells are still index-only afterwards.
func TestPreparedCompactionLeavesTheBranchCacheUnchanged(t *testing.T) {
	manager := openIndexOnlySession(t)
	before := manager.GetBranch("")
	lazyBefore := 0
	for _, entry := range before {
		if entry.lazy {
			lazyBefore++
		}
	}
	if lazyBefore == 0 {
		t.Fatal("the session did not load index-only; the test would not reach the hydration")
	}
	_ = manager.PrepareCompaction(CompactionSettings{Enabled: true})
	after := manager.GetBranch("")
	hydrated := 0
	for _, entry := range after {
		if !entry.lazy {
			hydrated++
		}
	}
	if hydrated > 0 {
		t.Fatalf("%d of %d cached shells are no longer lazy: the compaction hydrated the shared branch cache", hydrated, len(after))
	}
}
