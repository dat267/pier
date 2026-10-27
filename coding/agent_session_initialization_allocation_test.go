package coding

import (
	"fmt"
	"runtime"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts getEntryCount()
// uses the existing ID index rather than copying the entry array. D237's
// initial-tool declaration only needs to know whether a prior entry exists.
func TestAgentSessionInitializationDoesNotCopyHistory(t *testing.T) {
	const count = 512
	version := CurrentSessionVersion
	cwd := t.TempDir()
	entries := make([]FileEntry, count+1)
	entries[0].Header = &SessionHeader{Version: &version, ID: "session", Cwd: cwd}
	for i := range count {
		entries[i+1].Entry = &SessionEntry{Type: "custom", ID: fmt.Sprint(i), CustomType: "test"}
	}
	manager := InMemorySession(cwd, nil, entries)
	config := &SessionConfig{Cwd: cwd, Sessions: manager, StreamFn: mockSessionStreamFn()}
	warm, err := NewAgentSession(config)
	if err != nil {
		t.Fatal(err)
	}
	warm.Dispose()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	session, err := NewAgentSession(config)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	// Allow ordinary agent/control allocation, but not a 225 KiB snapshot
	// of history just to test whether it is empty.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64*1024 {
		t.Fatalf("initializing over %d entries allocated %d bytes, want at most 65536", count, allocated)
	}
	if len(manager.GetEntries()) != count {
		t.Fatal("initialization appended an initial declaration to a nonempty session")
	}
}
