package coding

import (
	"os/exec"
	"runtime"
)

// Port of utils/open-browser.ts: open a URL or file in the platform browser or
// default handler.

// BrowserCommand is the launcher and arguments for a platform's default
// handler.
func BrowserCommand(target string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{target}
	case "windows":
		// Never `cmd /c start`: cmd.exe re-parses metacharacters (&, |, ^, ...)
		// before start runs, which would make attacker-controlled URLs
		// injectable.
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		return "xdg-open", []string{target}
	}
}

// OpenBrowser opens target in the platform browser/default handler. It never
// invokes a shell, and the launch is best-effort: a missing launcher is
// ignored, because callers still present the target to the user.
func OpenBrowser(target string) {
	name, args := BrowserCommand(target)
	command := exec.Command(name, args...)
	if err := command.Start(); err != nil {
		return
	}
	// Detach: the launcher's exit is not awaited and its process is released.
	_ = command.Process.Release()
}
