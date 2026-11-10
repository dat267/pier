package coding

import (
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
