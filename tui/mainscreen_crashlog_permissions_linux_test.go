//go:build linux

package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// pi v1.4.0 packages/tui/src/tui-main-screen.ts writes this diagnostic
// log with Node's default mode; Pier protects its captured rendered content.
func TestMainScreenCrashLogUsesPrivateMode(t *testing.T) {
	dir := t.TempDir()
	writeCrash := func() string {
		screen := NewMainScreen(&recordingTerminal{width: 10, height: 3}, false, dir)
		if err := screen.reportOverwideLine([]string{"Bearer secret"}, 0, 10); err == nil {
			t.Fatal("expected overwide-line error")
		}
		return filepath.Join(dir, "pi-tui-crash.log")
	}

	path := writeCrash()
	checkPrivateCrashLogMode(t, path)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	writeCrash()
	checkPrivateCrashLogMode(t, path)
}

func checkPrivateCrashLogMode(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("TUI crash log mode = %#o, want %#o", got, 0o600)
	}
}
