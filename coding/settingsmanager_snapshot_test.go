package coding

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingSettingsStorage parks a global-scope write until released.
type blockingSettingsStorage struct {
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSettingsStorage) WithLock(scope SettingsScope, fn func(current *string) *string) error {
	if scope == SettingsScopeGlobal && s.armed.Load() {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	fn(nil)
	return nil
}

// Getters read the merged settings without the manager lock, so a setter
// whose synchronous persist is stuck in storage (no PersistQueue wired) does
// not stall readers such as the footer or the renderer, and they see the new
// value as soon as the setter has merged it. Upstream settings-manager.ts
// (pi v1.5.0 f9a26365) reads this.settings on one event loop with no lock.
func TestSettingsGettersDoNotWaitForAPersistInProgress(t *testing.T) {
	storage := &blockingSettingsStorage{entered: make(chan struct{}), release: make(chan struct{})}
	manager := newSettingsManagerFromStorage(storage, SettingsManagerCreateOptions{}, nil)
	storage.armed.Store(true) // after the constructor's loads

	setDone := make(chan struct{})
	go func() {
		defer close(setDone)
		manager.SetTheme("dark")
	}()
	<-storage.entered

	read := make(chan *string, 1)
	go func() { read <- manager.GetTheme() }()
	select {
	case theme := <-read:
		if theme == nil || *theme != "dark" {
			t.Fatalf("theme during the persist = %v, want the merged value dark", theme)
		}
	case <-time.After(2 * time.Second):
		close(storage.release)
		t.Fatal("GetTheme waited for the persist holding the manager lock")
	}
	close(storage.release)
	<-setDone
}

// The enabled-model list a getter returns is the shared settings snapshot, so
// it carries no spare capacity: a caller extending it (as
// addPersistedDefaultToNonEmptyScope does) must not write into the array
// every other reader holds. Upstream getEnabledModels (settings-manager.ts,
// v1.5.0 f9a26365) returns the array on a single event loop.
func TestEnabledModelsSnapshotCannotBeExtendedInPlace(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	// Decoding grows the slice, so three patterns typically leave spare room.
	writeSettingsFile(t, filepath.Join(agentDir, "settings.json"),
		`{"enabledModels":["anthropic/a","anthropic/b","anthropic/c"]}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	got := manager.GetEnabledModels()
	if len(got) != 3 || cap(got) != len(got) {
		t.Fatalf("enabled models len=%d cap=%d, want a clipped three-element snapshot", len(got), cap(got))
	}
	extended := append(got, "anthropic/d")
	extended[0] = "changed"
	if again := manager.GetEnabledModels(); len(again) != 3 || again[0] != "anthropic/a" {
		t.Fatalf("snapshot after a caller extended it = %v", again)
	}
	if empty := NewInMemorySettingsManager(&Settings{}, SettingsManagerCreateOptions{}).GetEnabledModels(); empty != nil {
		t.Fatalf("unset enabled models = %#v, want nil", empty)
	}
}
