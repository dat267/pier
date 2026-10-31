//go:build linux

package coding

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pi v1.4.0 utils/shell.ts runs `which`/`where` with `timeout: 5000` so a hung
// lookup is killed and treated as not found. The port must bound it too.
func TestFindExecutableOnPathTimesOut(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "which")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexec sleep 2\nprintf '/late/path\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	previous := shellLookupTimeout
	shellLookupTimeout = 100 * time.Millisecond
	t.Cleanup(func() { shellLookupTimeout = previous })

	start := time.Now()
	got := findExecutableOnPath("tool")
	elapsed := time.Since(start)
	if got != "" {
		t.Fatalf("which output after the timeout = %q, want no path", got)
	}
	if elapsed > time.Second {
		t.Fatalf("lookup took %v, want it bounded by the timeout", elapsed)
	}
}
