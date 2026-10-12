//go:build linux

package coding

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Upstream settings-manager.ts writes settings with Node's default mode.
func TestSettingsFileUsesNodeDefaultMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	root := t.TempDir()
	storage := NewFileSettingsStorage(filepath.Join(root, "project"), filepath.Join(root, "agent"))
	value := `{"defaultModel":"m"}`
	if err := storage.WithLock(SettingsScopeGlobal, func(*string) *string { return &value }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(storage.GlobalSettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Fatalf("settings mode = %#o; want Node default %#o", got, 0o666)
	}
	dirInfo, err := os.Stat(filepath.Dir(storage.GlobalSettingsPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Fatalf("settings directory mode = %#o; want Node default %#o", got, 0o777)
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
