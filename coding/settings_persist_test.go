package coding

import (
	"encoding/json"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/offloop"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

// Tests ported from packages/coding-agent/test/settings-manager.test.ts (and
// the escaping/migration cases from the -bug/-compaction suites).

// SetX hands persistence to the off-loop queue: the calling goroutine (the UI
// loop in interactive mode) must not wait on the storage lock, whose retries
// can cost up to 10 x 20 ms, or on the file read and write.
func TestSettingsSetDoesNotBlockOnTheStorageLock(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")
	writeFileMkdir(t, settingsPath, `{"theme":"light"}`)
	// Hold the storage lock the way a concurrent writer would.
	lock, err := acquireLockWithRetry(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()

	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{
		PersistQueue: offloop.New(),
	})
	done := make(chan struct{})
	go func() {
		manager.SetTheme("dark")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("SetTheme blocked on the storage lock")
	}
	// The queued persist fails after its retry window and the error is
	// recorded; nothing is lost or silently swallowed.
	manager.FlushPersists()
	if saved := readSettingsFile(t, settingsPath); saved["theme"] != "light" {
		t.Fatalf("theme = %#v, want the unchanged light", saved["theme"])
	}
	errors := manager.DrainErrors()
	if len(errors) != 1 || errors[0].Scope != SettingsScopeGlobal {
		t.Fatalf("errors = %+v", errors)
	}
}

// Persistence still lands: mutators keep the in-memory view synchronous and
// the queue persists every submission in order.
func TestSettingsPersistLandsOffLoop(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")
	writeFileMkdir(t, settingsPath, `{"theme":"light"}`)

	queue := offloop.New()
	defer queue.Stop()
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{
		PersistQueue: queue,
	})
	manager.SetTheme("dark")
	manager.SetDefaultThinkingLevel("high")
	manager.FlushPersists()
	saved := readSettingsFile(t, settingsPath)
	if saved["theme"] != "dark" || saved["defaultThinkingLevel"] != "high" {
		t.Fatalf("saved = %#v", saved)
	}
	if manager.GetTheme() == nil || *manager.GetTheme() != "dark" {
		t.Fatalf("in-memory theme = %v", manager.GetTheme())
	}
}

// The persisted file must not gain null-valued keys, because every write
// rewrites the whole document and the user would see keys they never set.
func TestSettingsPersistWritesNoNullKeys(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	settingsPath := filepath.Join(agentDir, "settings.json")
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})
	manager.SetDefaultThinkingLevel("high")

	saved := readSettingsFile(t, settingsPath)
	if saved["defaultThinkingLevel"] != "high" {
		t.Fatalf("saved = %#v", saved)
	}
	for key, value := range saved {
		if value == nil {
			t.Errorf("settings.json wrote %q: null", key)
		}
	}
}

type queuedFailureSettingsStorage struct {
	contents     map[SettingsScope]string
	writes       int
	firstWrite   chan struct{}
	releaseFirst chan struct{}
}

func (s *queuedFailureSettingsStorage) WithLock(scope SettingsScope, fn func(current *string) *string) error {
	var current *string
	if text, ok := s.contents[scope]; ok {
		current = &text
	}
	next := fn(current)
	if next == nil {
		return nil
	}
	s.writes++
	switch s.writes {
	case 1:
		close(s.firstWrite)
		<-s.releaseFirst
	case 2:
		return os.ErrPermission
	}
	s.contents[scope] = *next
	return nil
}

// Reference: pi v1.4.0 (4a080252), settings-manager.ts enqueueWrite/save.
// D280 retains later dirty fields when an older queued task succeeds.
func TestSettingsRetryRetainsChangesAfterOverlappingQueuedSaveFails(t *testing.T) {
	storage := &queuedFailureSettingsStorage{
		contents:     map[SettingsScope]string{SettingsScopeGlobal: `{"theme":"light"}`},
		firstWrite:   make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	queue := offloop.New()
	defer queue.Stop()
	manager := newSettingsManagerFromStorage(storage, SettingsManagerCreateOptions{PersistQueue: queue}, nil)

	manager.SetTheme("dark")
	<-storage.firstWrite
	manager.SetDefaultThinkingLevel("high")
	close(storage.releaseFirst)
	manager.FlushPersists()
	if failures := manager.DrainErrors(); len(failures) != 1 {
		t.Fatalf("write failures = %+v, want one", failures)
	}

	manager.SetDefaultProvider("openai")
	manager.FlushPersists()
	var saved map[string]any
	if err := json.Unmarshal([]byte(storage.contents[SettingsScopeGlobal]), &saved); err != nil {
		t.Fatal(err)
	}
	if saved["defaultThinkingLevel"] != "high" || saved["theme"] != "dark" || saved["defaultProvider"] != "openai" {
		t.Fatalf("next save did not retain settings: %#v", saved)
	}
}

// D280: an earlier successful theme snapshot must not clear a newer value of
// that same setting if the newer write fails (pi v1.4.0 settings-manager.ts).
func TestSettingsRetryRetainsNewerValueAfterQueuedSaveFails(t *testing.T) {
	storage := &queuedFailureSettingsStorage{
		contents:     map[SettingsScope]string{SettingsScopeGlobal: `{"theme":"light"}`},
		firstWrite:   make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	queue := offloop.New()
	defer queue.Stop()
	manager := newSettingsManagerFromStorage(storage, SettingsManagerCreateOptions{PersistQueue: queue}, nil)

	manager.SetTheme("dark")
	<-storage.firstWrite
	manager.SetTheme("blue")
	close(storage.releaseFirst)
	manager.FlushPersists()
	if failures := manager.DrainErrors(); len(failures) != 1 {
		t.Fatalf("write failures = %+v, want one", failures)
	}

	manager.SetDefaultProvider("openai")
	manager.FlushPersists()
	var saved map[string]any
	if err := json.Unmarshal([]byte(storage.contents[SettingsScopeGlobal]), &saved); err != nil {
		t.Fatal(err)
	}
	if saved["theme"] != "blue" || saved["defaultProvider"] != "openai" {
		t.Fatalf("next save did not retry newer setting value: %#v", saved)
	}
}

// pi v1.4.0 (4a080252), settings-manager.ts persistScopedSettings lets
// JSON.parse failures reach enqueueWrite's diagnostic path, retaining dirty
// fields for retry rather than acknowledging a skipped write.
func TestSettingsSaveReportsExternalMalformedJSONAndRetries(t *testing.T) {
	agentDir, projectDir := settingsDirs(t)
	path := filepath.Join(agentDir, "settings.json")
	writeFileMkdir(t, path, `{"theme":"light"}`)
	manager := NewSettingsManagerFromFiles(projectDir, agentDir, SettingsManagerCreateOptions{})

	const malformed = `{"theme":`
	writeFileMkdir(t, path, malformed)
	manager.SetTheme("dark")
	failures := manager.DrainErrors()
	if len(failures) != 1 || failures[0].Scope != SettingsScopeGlobal || failures[0].Path != path {
		t.Errorf("malformed external settings must report a save error: %+v", failures)
	}
	data := readFileBytes(t, path)
	if string(data) != malformed {
		t.Fatalf("malformed external settings were overwritten: %s", data)
	}

	writeFileMkdir(t, path, `{"theme":"light"}`)
	manager.SetDefaultProvider("openai")
	saved := readSettingsFile(t, path)
	if saved["theme"] != "dark" || saved["defaultProvider"] != "openai" {
		t.Fatalf("recovered save did not retry skipped settings: %#v", saved)
	}
}

// D283: pi v1.4.0 (4a080252), core/settings-manager.ts withLock reads
// missing files before locking. Go callbacks can overlap (unlike JS's
// synchronous callbacks), so even the first read must be locked.
func TestFileSettingsStorageConcurrentFirstWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-agent", "settings.json")
	synctest.Test(t, func(t *testing.T) {
		errs := make(chan error, 2)
		for _, key := range []string{"first", "second"} {
			go func() {
				storage := &FileSettingsStorage{GlobalSettingsPath: path}
				errs <- storage.WithLock(SettingsScopeGlobal, func(current *string) *string {
					settings := map[string]bool{}
					if current != nil {
						if err := json.Unmarshal([]byte(*current), &settings); err != nil {
							t.Error(err)
						}
					}
					// Hold the transaction open so an unlocked first read overlaps.
					time.Sleep(40 * time.Millisecond)
					settings[key] = true
					data, err := ai.MarshalJSON(settings)
					if err != nil {
						t.Error(err)
					}
					text := string(data)
					return &text
				})
			}()
		}
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
	})
	got := readSettingsFile(t, path)
	if got["first"] != true || got["second"] != true {
		t.Fatalf("concurrent first writes lost an update: %#v", got)
	}
}
