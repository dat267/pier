//go:build linux

package interactive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// session-selector.ts decodes trash stderr with encoding: "utf-8".
func TestDeleteSessionFileReplacesMalformedUTF8InTrashHint(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "trash")
	// Fail and print an invalid byte, so the fallback unlink runs.
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '\\377' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// A non-empty directory cannot be removed by os.Remove, so the fallback
	// fails and the hint is included in the error.
	dir := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := DeleteSessionFile(dir)
	if result.OK {
		t.Fatal("delete unexpectedly succeeded")
	}
	if !strings.Contains(result.Error, "\uFFFD") {
		t.Fatalf("error = %q; want replacement-decoded hint", result.Error)
	}
}
