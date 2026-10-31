package interactive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Reference: pi v1.4.0 (4a080252), theme/theme.ts startThemeWatcher excludes
// SYSTEM_THEME_NAME. A file named system.json must never replace the generated
// system theme when it changes.
func TestSystemThemeDoesNotWatchAFileNamedSystem(t *testing.T) {
	StopThemeWatcher()
	SetRegisteredThemes(nil)
	SetCustomThemesDir(t.TempDir())
	OnThemeChange(func() {})
	t.Cleanup(func() {
		StopThemeWatcher()
		SetRegisteredThemes(nil)
		SetCustomThemesDir("")
	})

	dir := CustomThemesDir()
	file := filepath.Join(dir, "system.json")
	writeThemeFile(t, dir, "system.json", SystemThemeName, "#123456")
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	if ok, message := SetTheme(SystemThemeName, true); !ok {
		t.Fatalf("SetTheme(system) failed: %s", message)
	}
	// Let the watcher take its initial mtime snapshot before changing the file.
	time.Sleep(300 * time.Millisecond)
	writeThemeFile(t, dir, "system.json", SystemThemeName, "#abcdef")

	deadline := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := CurrentTheme().SourcePath; got != "" {
			t.Fatalf("system theme was replaced by watched file %q", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
