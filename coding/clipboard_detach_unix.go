//go:build unix

package coding

import (
	"os/exec"
	"syscall"
)

// detachClipboardCommand puts the helper in its own process group, so a
// terminal suspend (SIGTSTP to pi's group, Ctrl+Z) leaves the clipboard daemon
// running (upstream spawn's `detached`, v1.4.0 f8ab362cc, #11179).
func detachClipboardCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
