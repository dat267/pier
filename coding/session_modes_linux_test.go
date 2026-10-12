//go:build linux

package coding

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// session-manager.ts uses Node's default modes: 0777 for directories and 0666
// for session files, each filtered by the process umask.
func TestSessionFileUsesNodeDefaultMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	file := filepath.Join(t.TempDir(), "session.jsonl")
	if err := writeSessionEntries(file, nil); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o666 {
		t.Fatalf("session file mode = %#o; want Node default %#o", got, 0o666)
	}
}

func TestForkedSessionUsesNodeDefaultModes(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	root := t.TempDir()
	forkDir := filepath.Join(root, "forked")
	manager, err := writeForkedSession(filepath.Join(root, "project"), forkDir, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(manager.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o666 {
		t.Errorf("forked session mode = %#o; want Node default %#o", got, 0o666)
	}
	dirInfo, err := os.Stat(forkDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Errorf("forked session directory mode = %#o; want Node default %#o", got, 0o777)
	}
}

func TestSessionManagerUsesNodeDefaultDirectoryMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	dir := filepath.Join(t.TempDir(), "sessions")
	_ = NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: dir})
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o777 {
		t.Fatalf("session manager directory mode = %#o; want Node default %#o", got, 0o777)
	}
}

func TestSessionExportUsesNodeDefaultDirectoryMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: boolPtr(false)})
	dir := filepath.Join(t.TempDir(), "exports")
	if _, err := ExportSessionToJsonl(manager, filepath.Join(dir, "export.jsonl"), nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o777 {
		t.Fatalf("export directory mode = %#o; want Node default %#o", got, 0o777)
	}
}

func TestSessionExportUsesNodeDefaultFileMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: boolPtr(false)})
	path := filepath.Join(t.TempDir(), "export.jsonl")
	if _, err := ExportSessionToJsonl(manager, path, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Fatalf("export file mode = %#o; want Node default %#o", got, 0o666)
	}
}

func TestSessionDirectoryUsesNodeDefaultMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	root := t.TempDir()
	dir := DefaultSessionDir(filepath.Join(root, "project"), filepath.Join(root, "agent"))
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Fatalf("session directory mode = %#o; want Node default %#o", got, 0o777)
	}
}
