//go:build linux

package coding

import (
	"context"
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
	dirInfo, err := os.Stat(filepath.Dir(storage.GlobalSettingsPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("global settings directory mode = %#o, want %#o", got, 0o700)
	}
	if err := os.Chmod(storage.GlobalSettingsPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(storage.GlobalSettingsPath), 0o777); err != nil {
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
	dirInfo, err = os.Stat(filepath.Dir(storage.GlobalSettingsPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("replaced global settings directory mode = %#o, want %#o", got, 0o700)
	}
}

// pi v1.4.0 packages/coding-agent/src/core/auth-storage.ts requests mode
// 0700 only while creating a missing auth parent; an existing public dir stays public.
func TestAuthStorageHardensExistingParentDirectory(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	agentDir := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(agentDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(agentDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAuthStorage(filepath.Join(agentDir, "auth.json")).List(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("auth parent directory mode = %#o, want %#o", got, 0o700)
	}
}

// pi v1.4.0 packages/coding-agent/src/core/trust-manager.ts uses Node defaults;
// Pier keeps the trust decision and project paths owner-only.
func TestTrustFileUsesPrivateMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	store := NewProjectTrustStore(filepath.Join(t.TempDir(), "agent"))
	trusted := true
	if err := store.Set(t.TempDir(), &trusted); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.TrustPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("trust file mode = %#o, want %#o", got, 0o600)
	}
	if err := os.Chmod(store.TrustPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	trusted = false
	if err := store.Set(t.TempDir(), &trusted); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(store.TrustPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("replaced trust file mode = %#o, want %#o", got, 0o600)
	}
	dirInfo, err := os.Stat(filepath.Dir(store.TrustPath()))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("trust directory mode = %#o, want %#o", got, 0o700)
	}
}

// pi v1.4.0 packages/coding-agent/src/core/crash-log.ts writes crash logs
// with Node's default mode; Pier protects potentially sensitive error details.
func TestCrashLogUsesPrivateMode(t *testing.T) {
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
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("crash log mode = %#o, want %#o", got, 0o600)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if RecordCrash(CrashInput{Error: errors.New("replacement")}, path) == nil {
		t.Fatal("replacement crash record was not persisted")
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("replaced crash log mode = %#o, want %#o", got, 0o600)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Fatalf("crash log directory mode = %#o; want Node default %#o", got, 0o777)
	}
}
