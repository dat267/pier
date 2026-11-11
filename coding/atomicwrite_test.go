package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// readOnlyDir makes dir reject new entries, which is how these tests make an
// atomic replace impossible without a full filesystem: the temp file cannot be
// created. The permission is restored before t.TempDir's own cleanup runs.
func readOnlyDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

// TestSessionRewriteKeepsThePreviousFileOnFailure pins A506: a whole-file
// session rewrite truncates the target before writing it, so a write that
// cannot complete leaves an empty or half-written session — the transcript is
// gone. The rewrite must publish a new file instead, leaving the previous one
// readable when it cannot.
func TestSessionRewriteKeepsThePreviousFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	path := filepath.Join(dir, "session.jsonl")

	previous := []byte("{\"type\":\"session\",\"version\":3}\n{\"type\":\"message\"}\n")
	if err := writePrivateSessionFile(path, previous); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	readOnlyDir(t, dir)
	err := writePrivateSessionFile(path, []byte("{\"type\":\"session\"}\n"))

	// The read-only directory is what makes the publish impossible: a rewrite
	// must not fall back to overwriting the target in place.
	if err == nil {
		t.Fatalf("a rewrite that cannot create its new file reported success")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the failed rewrite removed the session file: %v", readErr)
	}
	if string(got) != string(previous) {
		t.Errorf("the failed rewrite left %q, want the previous content %q", got, previous)
	}
}

// TestPrivateFileWriteKeepsThePreviousContentOnFailure covers the same root
// cause for the private files the port rewrites in place: trust.json, mcp.json
// and the crash and debug logs.
func TestPrivateFileWriteKeepsThePreviousContentOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	path := filepath.Join(dir, "mcp.json")

	previous := []byte("{\"mcpServers\":{\"srv\":{\"command\":\"srv\"}}}\n")
	if err := WritePrivateFile(path, previous); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	readOnlyDir(t, dir)
	if err := WritePrivateFile(path, []byte("{}\n")); err == nil {
		t.Fatalf("a write that cannot create its new file reported success")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the failed write removed the config: %v", readErr)
	}
	if string(got) != string(previous) {
		t.Errorf("the failed write left %q, want the previous content %q", got, previous)
	}
}

// TestAuthWriteKeepsThePreviousFileOnFailure pins A507 for auth.json: the store
// opened it with O_TRUNC, so a failed save emptied the credentials first. OAuth
// refresh tokens cannot be derived again, so the previous file must survive.
func TestAuthWriteKeepsThePreviousFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	path := filepath.Join(dir, "auth.json")

	previous := `{"anthropic":{"type":"oauth","refresh":"keep-me"}}`
	if err := writeFileMode(path, previous, authFileWriteMode); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	readOnlyDir(t, dir)
	if err := writeFileMode(path, `{}`, authFileWriteMode); err == nil {
		t.Fatalf("a save that cannot create its new file reported success")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the failed save removed auth.json: %v", readErr)
	}
	if string(got) != previous {
		t.Errorf("the failed save left %q, want the previous credentials", got)
	}
}

// TestProjectSettingsWriteKeepsThePreviousFileOnFailure covers the project-scope
// settings write, which used os.WriteFile like the global branch did before
// D454.
func TestProjectSettingsWriteKeepsThePreviousFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	path := filepath.Join(dir, "settings.json")

	previous := `{"theme":"flat"}`
	if err := saveSettingsContent(SettingsScopeProject, path, previous); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	readOnlyDir(t, dir)
	if err := saveSettingsContent(SettingsScopeProject, path, `{}`); err == nil {
		t.Fatalf("a save that cannot create its new file reported success")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the failed save removed the settings: %v", readErr)
	}
	if string(got) != previous {
		t.Errorf("the failed save left %q, want the previous settings", got)
	}
}

// TestAtomicReplaceKeepsTheRequestedMode checks that the mode argument behaves
// like os.WriteFile's: the umask still applies, so a project file is not made
// world-writable by the replace.
func TestAtomicReplaceKeepsTheRequestedMode(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []os.FileMode{0o600, 0o644, 0o666} {
		target := filepath.Join(dir, fmt.Sprintf("target-%v", mode))
		reference := filepath.Join(dir, fmt.Sprintf("reference-%v", mode))
		if err := WritePrivateFile(target, []byte("x")); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.WriteFile(reference, []byte("x"), mode); err != nil {
			t.Fatalf("reference: %v", err)
		}
		if err := replaceFileAtomically(target, []byte("y"), mode); err != nil {
			t.Fatalf("replace at %v: %v", mode, err)
		}
		got, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.Stat(reference)
		if err != nil {
			t.Fatal(err)
		}
		if got.Mode().Perm() != want.Mode().Perm() {
			t.Errorf("replace at %v produced mode %v, want %v (os.WriteFile's umask result)",
				mode, got.Mode().Perm(), want.Mode().Perm())
		}
	}
}
