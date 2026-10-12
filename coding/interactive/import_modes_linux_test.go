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

// Upstream node:fs copyFileSync preserves the imported session's permissions.
func TestImportSessionDirectoryUsesNodeDefaultMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	app, cleanup := newTestApp(t)
	defer cleanup()
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	persist := false
	app.sessionMgr = coding.NewSessionManager(app.sessionMgr.GetCwd(), &coding.SessionManagerOptions{
		SessionDir: sessionDir, Persist: &persist,
	})
	source := writeImportableSession(t, t.TempDir())
	newCommandWiring(app).HandleImportCommand(context.Background(), "/import "+source)
	answerConfirm(t, app, 0)

	info, err := os.Stat(sessionDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o777 {
		t.Fatalf("import session directory mode = %#o; want Node default %#o", got, 0o777)
	}
}

func TestImportSessionPreservesSourceMode(t *testing.T) {
	oldMask := syscall.Umask(0o022)
	defer syscall.Umask(oldMask)

	app, cleanup := newTestApp(t)
	defer cleanup()
	sessionDir := withSessionDir(t, app)
	source := writeImportableSession(t, t.TempDir())
	if err := os.Chmod(source, 0o644); err != nil {
		t.Fatal(err)
	}
	// copyFileSync preserves the source mode rather than applying the current umask.
	syscall.Umask(0o077)
	newCommandWiring(app).HandleImportCommand(context.Background(), "/import "+source)
	answerConfirm(t, app, 0)

	copied := filepath.Join(sessionDir, filepath.Base(source))
	info, err := os.Stat(copied)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("imported session mode = %#o; want source mode 0644", got)
	}
}
