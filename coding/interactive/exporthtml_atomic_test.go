package interactive

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExportWritePublishesAtomically pins A506: /export wrote its target with
// O_TRUNC, so exporting over an existing file destroyed that file before the new
// HTML was durable. The write now goes through coding.WritePrivateFile, which
// renames a fully written temp file into place.
func TestExportWritePublishesAtomically(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	path := filepath.Join(dir, "notes.html")

	previous := []byte("<p>previous export</p>\n")
	if err := writePrivateFile(path, previous); err != nil {
		t.Fatalf("initial write: %v", err)
	}
	// The successful path publishes the new content and keeps no debris.
	if err := writePrivateFile(path, []byte("<p>second export</p>\n")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "<p>second export</p>\n" {
		t.Fatalf("second write left %q (%v)", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "notes.html" {
		t.Errorf("the export left debris beside its target: %v", entries)
	}

	// A write that cannot publish must fail without touching the target.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := writePrivateFile(path, []byte("<p>third export</p>\n")); err == nil {
		t.Fatalf("a write that cannot create its new file reported success")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the failed export removed the target: %v", readErr)
	}
	if string(got) != "<p>second export</p>\n" {
		t.Errorf("the failed export left %q, want the previous content", got)
	}
}
