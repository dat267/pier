package coding

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// pi 4a080252 packages/coding-agent/src/utils/open-browser.ts uses spawn().unref():
// launching is nonblocking, but Node still reaps the launcher when it exits.
func TestOpenBrowserReapsExitedLauncher(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "launcher-pid")
	name, _ := BrowserCommand(pidFile)
	script := "#!/bin/sh\nprintf '%s' \"$$\" > \"$1.part\" && mv \"$1.part\" \"$1\"\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	OpenBrowser(pidFile)

	var pid int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, err = strconv.Atoi(string(data))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		time.Sleep(time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("launcher did not publish its PID")
	}
	// Reap a leaked child even on the red path. Do not race the production
	// waiter while observing whether the process disappears on its own.
	t.Cleanup(func() {
		if pid != 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			_, _ = syscall.Wait4(pid, nil, 0, nil)
		}
	})
	statPath := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	var stat []byte
	for time.Now().Before(deadline) {
		var err error
		stat, err = os.ReadFile(statPath)
		if errors.Is(err, os.ErrNotExist) {
			pid = 0
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	state := strings.Fields(string(stat))
	t.Fatalf("launcher %d was not reaped after exit: %v", pid, state)
}
