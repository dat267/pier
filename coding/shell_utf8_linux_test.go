//go:build linux

package coding

import (
	"os"
	"path/filepath"
	"testing"
)

// utils/shell.ts runs which with encoding: "utf-8".
func TestFindExecutableOnPathReplacesMalformedUTF8(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "which")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '/path/\\377tool\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got := findExecutableOnPath("tool"); got != "/path/\uFFFDtool" {
		t.Fatalf("which output = %q; want replacement-decoded path", got)
	}
}
