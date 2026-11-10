package interactive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A theme switch made while the custom-theme watcher is reloading its file
// stands: the reload must not overwrite it. Upstream theme.ts (v1.5.0
// f9a26365) checks currentThemeName and swaps the theme synchronously in its
// fs.watch callback, so a switch cannot land in between.
func TestThemeSwitchDuringAWatcherReloadStands(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	writeThemeFile(t, dir, "mytheme.json", "mytheme", "#ff0000")
	entered := make(chan struct{})
	release := make(chan struct{})
	original := themeWatcherLoad
	themeWatcherLoad = func(path string, mode ColorMode) (*Theme, error) {
		close(entered)
		<-release
		return original(path, mode)
	}
	t.Cleanup(func() {
		StopThemeWatcher()
		themeWatcherLoad = original
		InitTheme("dark", false)
	})

	InitTheme("mytheme", false)
	if CurrentTheme() == nil || CurrentTheme().Name != "mytheme" {
		t.Fatalf("theme = %+v", CurrentTheme())
	}
	startThemeWatcher("mytheme")
	// Keep moving the mtime forward: the watcher records its baseline when its
	// goroutine starts, which may be after a single touch.
	giveUp := time.After(5 * time.Second)
	for step := 1; ; step++ {
		later := time.Now().Add(time.Duration(step) * time.Minute)
		if err := os.Chtimes(filepath.Join(dir, "mytheme.json"), later, later); err != nil {
			t.Fatal(err)
		}
		select {
		case <-entered:
		case <-time.After(50 * time.Millisecond):
			continue
		case <-giveUp:
			t.Fatal("the watcher did not reload the touched file")
		}
		break
	}
	if ok, message := SetTheme("dark", false); !ok {
		t.Fatalf("SetTheme = %q", message)
	}
	close(release)
	// The reload publishes immediately after its load returns, if it does.
	deadline := time.After(200 * time.Millisecond)
	for {
		if current := CurrentTheme(); current == nil || current.Name != "dark" {
			t.Fatalf("theme = %v after the switch to dark; the reload overwrote it", current.Name)
		}
		select {
		case <-deadline:
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}
