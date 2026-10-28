package coding

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/ai"
)

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
