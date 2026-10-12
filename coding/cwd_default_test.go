package coding

import (
	ctxpkg "context"
	"os"
	"path/filepath"
	"testing"
)

// Upstream core/sdk.ts:176 defaults a session to process.cwd() when neither
// options.cwd nor a session manager supplies one. The port's os.Getwd fallback
// was shadowed, so cwd stayed "" and NewSessionManager resolved it to ".".
func TestCreateAgentSessionDefaultsToPhysicalCwd(t *testing.T) {
	physical, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "cwd-link")
	if err := os.Symlink(physical, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// os.Getwd prefers a valid $PWD, so this reproduces the logical-path case
	// the boot fix canonicalizes.
	t.Setenv("PWD", link)
	if wd, err := os.Getwd(); err != nil || wd != link {
		t.Skipf("os.Getwd does not honor $PWD here (got %q, %v)", wd, err)
	}

	created, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{AgentDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	want := CanonicalizePath(physical)
	if got := created.Session.Sessions.GetCwd(); got != want {
		t.Fatalf("cwd = %q, want the physical %q", got, want)
	}
}

// OpenSession's cwd fallback for a header without one must resolve the physical
// directory like Node's process.cwd(), not the logical $PWD.
func TestOpenSessionHeaderlessCwdIsPhysical(t *testing.T) {
	physical, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "cwd-link")
	if err := os.Symlink(physical, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("PWD", link)
	if wd, err := os.Getwd(); err != nil || wd != link {
		t.Skipf("os.Getwd does not honor $PWD here (got %q, %v)", wd, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := `{"type":"session","version":3,"id":"s1","timestamp":"t0"}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	want := CanonicalizePath(physical)
	if got := m.GetCwd(); got != want {
		t.Fatalf("cwd = %q, want the physical %q", got, want)
	}
}
