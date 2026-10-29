//go:build linux

package coding

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// pi v1.4.0 packages/coding-agent/src/core/settings-manager.ts writes
// project settings with Node's default mode.
func TestProjectSettingsFileUsesNodeDefaultMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	root := t.TempDir()
	storage := NewFileSettingsStorage(filepath.Join(root, "project"), filepath.Join(root, "agent"))
	value := `{"defaultModel":"m"}`
	if err := storage.WithLock(SettingsScopeProject, func(*string) *string { return &value }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(storage.ProjectSettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Fatalf("settings mode = %#o; want Node default %#o", got, 0o666)
	}
	dirInfo, err := os.Stat(filepath.Dir(storage.ProjectSettingsPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Fatalf("settings directory mode = %#o; want Node default %#o", got, 0o777)
	}
}

// pi v1.4.0 packages/coding-agent/src/core/settings-manager.ts uses the Node
// default mode; global settings can contain proxy credentials and stay private.
func TestGlobalSettingsFileUsesPrivateMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	root := t.TempDir()
	storage := NewFileSettingsStorage(filepath.Join(root, "project"), filepath.Join(root, "agent"))
	value := `{"httpProxy":"http://user:secret@proxy"}`
	if err := storage.WithLock(SettingsScopeGlobal, func(*string) *string { return &value }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(storage.GlobalSettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("global settings mode = %#o, want %#o", got, 0o600)
	}
	if err := os.Chmod(storage.GlobalSettingsPath, 0o644); err != nil {
		t.Fatal(err)
	}
	value = `{"theme":"dark"}`
	if err := storage.WithLock(SettingsScopeGlobal, func(*string) *string { return &value }); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(storage.GlobalSettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("replaced global settings mode = %#o, want %#o", got, 0o600)
	}
}

// Upstream trust-manager.ts writes trust.json with Node's default mode.
func TestTrustFileUsesNodeDefaultMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	store := NewProjectTrustStore(t.TempDir())
	trusted := true
	if err := store.Set(t.TempDir(), &trusted); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.TrustPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Fatalf("trust file mode = %#o; want Node default %#o", got, 0o666)
	}
	dirInfo, err := os.Stat(filepath.Dir(store.TrustPath()))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Fatalf("trust directory mode = %#o; want Node default %#o", got, 0o777)
	}
}

// Upstream crash-log.ts writes crash logs with Node's default mode.
func TestCrashLogUsesNodeDefaultMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	path := filepath.Join(t.TempDir(), "agent", "crash.log")
	if RecordCrash(CrashInput{Error: errors.New("boom")}, path) == nil {
		t.Fatal("crash record was not persisted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Fatalf("crash log mode = %#o; want Node default %#o", got, 0o666)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Fatalf("crash log directory mode = %#o; want Node default %#o", got, 0o777)
	}
}
