package coding

import (
	"os"
	"path/filepath"
	"runtime"
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
