package interactive

import (
	"os"
	"path/filepath"
	"testing"
)

// D443: the delete unlinks the file instead of handing it to the `trash` CLI.
// A `trash` that stalls kept the picker frozen behind it, and the async version
// never finished at all, because the worker was stuck in the subprocess.
func TestDeleteSessionFileDoesNotRunTheTrashCLI(t *testing.T) {
	stubDir := t.TempDir()
	marker := filepath.Join(stubDir, "ran")
	stub := filepath.Join(stubDir, "trash")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := DeleteSessionFile(path)
	if !result.OK {
		t.Fatalf("delete failed: %+v", result)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session file still exists: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the trash CLI was run; the delete must unlink directly")
	}
	if result.Method != "unlink" {
		t.Fatalf("method = %q, want unlink", result.Method)
	}
}

// Deleting a file that is already gone counts as deleted, so a repeated delete
// is not an error.
func TestDeleteSessionFileMissingFileIsNotAnError(t *testing.T) {
	if result := DeleteSessionFile(filepath.Join(t.TempDir(), "gone.jsonl")); !result.OK {
		t.Fatalf("missing file reported an error: %+v", result)
	}
}

// A real removal failure is reported with its own message, with no trash hint
// appended.
func TestDeleteSessionFileReportsAnUnlinkFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := DeleteSessionFile(dir)
	if result.OK || result.Error == "" {
		t.Fatalf("removing a non-empty directory reported %+v", result)
	}
}
