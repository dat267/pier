//go:build linux

package interactive

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/dat267/pier/coding"
)

// pi v1.4.0 packages/coding-agent/src/core/agent-session-runtime.ts uses
// copyFileSync, preserving source mode;
// Pier's private import mode is the D289 security divergence.
func TestImportSessionDirectoryUsesPrivateMode(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	persist := false
	app.sessionMgr = coding.NewSessionManager(app.sessionMgr.GetCwd(), &coding.SessionManagerOptions{
		SessionDir: sessionDir, Persist: &persist,
	})
	source := writeImportableSession(t, t.TempDir())
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	newCommandWiring(app).HandleImportCommand(context.Background(), "/import "+source)
	answerConfirm(t, app, 0)

	info, err := os.Stat(sessionDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("import session directory mode = %#o; want private %#o", got, 0o700)
	}
}

func TestImportedSessionUsesPrivateMode(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	sessionDir := withSessionDir(t, app)
	source := writeImportableSession(t, t.TempDir())
	if err := os.Chmod(source, 0o644); err != nil {
		t.Fatal(err)
	}
	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)
	newCommandWiring(app).HandleImportCommand(context.Background(), "/import "+source)
	answerConfirm(t, app, 0)

	copied := filepath.Join(sessionDir, filepath.Base(source))
	info, err := os.Stat(copied)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("imported session mode = %#o; want private 0600", got)
	}
}
