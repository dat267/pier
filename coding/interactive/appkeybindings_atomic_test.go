package interactive

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKeybindingMigrationKeepsTheFileOnFailure pins A507 for keybindings.json:
// the migration wrote the file with os.WriteFile, so a failed migration could
// leave the user's bindings truncated.
func TestKeybindingMigrationKeepsTheFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	path := filepath.Join(dir, "keybindings.json")

	// A legacy name the migration rewrites.
	previous := []byte("{\n  \"submit\": \"ctrl+enter\"\n}\n")
	if err := os.WriteFile(path, previous, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if MigrateKeybindingsFile(path) {
		t.Fatalf("a migration that cannot publish reported success")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the failed migration removed keybindings.json: %v", readErr)
	}
	if string(got) != string(previous) {
		t.Errorf("the failed migration left %q, want the previous bindings", got)
	}
}
