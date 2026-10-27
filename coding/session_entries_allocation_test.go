package coding

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts getEntries()
// filters the header and returns a new array. Large resumed sessions call this
// during setup and transcript replay; growing a Go slice repeatedly copies the
// whole descriptor list several times before the first frame.
func TestGetEntriesAllocationBudget(t *testing.T) {
	const count = 512
	entries := make([]FileEntry, count+1)
	version := CurrentSessionVersion
	entries[0].Header = &SessionHeader{Version: &version, ID: "session", Cwd: t.TempDir()}
	for i := range count {
		entries[i+1].Entry = &SessionEntry{Type: "custom", ID: fmt.Sprint(i), CustomType: "test"}
	}
	manager := InMemorySession(entries[0].Header.Cwd, nil, entries)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	copied := manager.GetEntries()
	runtime.ReadMemStats(&after)
	if len(copied) != count {
		t.Fatalf("entries = %d, want %d", len(copied), count)
	}
	// Twice the final slice size allows allocator rounding but excludes
	// repeated allocation/copy of growing slices. No wall-clock threshold.
	budget := uint64(count) * uint64(reflect.TypeFor[SessionEntry]().Size()) * 2
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > budget {
		t.Fatalf("copying %d entries allocated %d bytes, want at most %d", count, allocated, budget)
	}
	copied[0].ID = "changed"
	if manager.GetEntries()[0].ID != "0" {
		t.Fatal("mutating a returned entry changed the manager's descriptor")
	}
	if got := InMemorySession(t.TempDir(), nil, nil).GetEntries(); got != nil {
		t.Fatalf("empty manager entries = %#v, want nil", got)
	}
}
