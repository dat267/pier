//go:build unix

package coding

import (
	"os/exec"
	"syscall"
)

// detachHelperCommand puts a helper (the clipboard daemon, the browser
// launcher) in a session of its own, as upstream spawn's `detached` does
// (libuv setsid()): a terminal suspend (SIGTSTP to pi's group, Ctrl+Z) leaves it
// running (v1.4.0 f8ab362cc, #11179), and it has no controlling terminal to
// read or to take the foreground of (pi v1.5.0 f9a26365 clipboard-command.ts,
// open-browser.ts).
func detachHelperCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
