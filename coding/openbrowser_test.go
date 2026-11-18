package coding

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBrowserCommand(t *testing.T) {
	name, args := BrowserCommand("https://example.com")
	switch runtime.GOOS {
	case "darwin":
		if name != "open" || len(args) != 1 || args[0] != "https://example.com" {
			t.Fatalf("darwin: %s %v", name, args)
		}
	case "windows":
		if name != "rundll32" || len(args) != 2 || args[0] != "url.dll,FileProtocolHandler" {
			t.Fatalf("windows: %s %v", name, args)
		}
	default:
		if name != "xdg-open" || len(args) != 1 || args[0] != "https://example.com" {
			t.Fatalf("unix: %s %v", name, args)
		}
	}
}

// TestOpenBrowserLaunchesHandler stubs the platform launcher on PATH and
// checks OpenBrowser spawns it with the target.
func TestOpenBrowserLaunchesHandler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the PATH stub is a shell script")
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "args")
	// Write the argument aside and rename it into place: the `>` redirection creates
	// the file before printf has written it, and printf is a separate process under
	// Android's /bin/sh (mksh), so a reader can watch the output file exist and be
	// empty for tens of milliseconds. Renaming publishes it only once it is complete,
	// which is what makes the check below a comparison rather than a race.
	script := "#!/bin/sh\nprintf '%s' \"$1\" > " + output + ".part && mv " + output + ".part " + output + "\n"
	name, _ := BrowserCommand("x")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	OpenBrowser("https://example.com/opened")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if content, err := os.ReadFile(output); err == nil {
			if string(content) != "https://example.com/opened" {
				t.Fatalf("args = %q", content)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the launcher was not invoked")
}

// pi v1.5.0 open-browser.ts launches from /: WSL's explorer.exe cannot start
// when the cwd's \\wsl.localhost\... path exceeds MAX_PATH.
func TestOpenBrowserLaunchesFromRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the PATH stub is a shell script")
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "cwd")
	script := "#!/bin/sh\nprintf '%s' \"$PWD\" > " + output + ".part && mv " + output + ".part " + output + "\n"
	name, _ := BrowserCommand("x")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	OpenBrowser("https://example.com/opened")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if content, err := os.ReadFile(output); err == nil {
			if string(content) != "/" {
				t.Fatalf("launcher cwd = %q, want /", content)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the launcher was not invoked")
}

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
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
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
