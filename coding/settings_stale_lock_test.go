package coding

import (
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

// Reference: pi v1.3.0 core/settings-manager.ts uses proper-lockfile's
// default 10-second stale directory recovery. Recover legacy Pier files too.
// A live holder refreshes its lease even while its callback runs.
func TestSettingsLiveLockDoesNotBecomeStale(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		storage := NewFileSettingsStorage(dir, dir)
		err := storage.WithLock(SettingsScopeGlobal, func(*string) *string {
			now := time.Now()
			if err := os.Chtimes(path+".lock", now, now); err != nil {
				t.Fatal(err)
			}
			time.Sleep(11 * time.Second)
			called := false
			err := storage.WithLock(SettingsScopeGlobal, func(*string) *string { called = true; return nil })
			if err == nil || called {
				t.Error("another writer reclaimed a live lock")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestSettingsSaveRecoversStaleLock(t *testing.T) {
	for _, directory := range []bool{true, false} {
		t.Run(map[bool]string{true: "directory", false: "legacy file"}[directory], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			lock := path + ".lock"
			if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			if directory {
				err = os.Mkdir(lock, 0700)
			} else {
				err = os.WriteFile(lock, nil, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-time.Minute)
			if err := os.Chtimes(lock, old, old); err != nil {
				t.Fatal(err)
			}
			storage := NewFileSettingsStorage(dir, dir)
			err = storage.WithLock(SettingsScopeGlobal, func(*string) *string { next := `{"theme":"light"}`; return &next })
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != `{"theme":"light"}` {
				t.Fatalf("saved=%s err=%v", got, err)
			}
			if _, err := os.Stat(lock); !os.IsNotExist(err) {
				t.Fatalf("lock remains: %v", err)
			}
		})
	}
}
