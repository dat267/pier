//go:build !windows

package mcp

import (
	"os"
	"os/exec"
	"syscall"
)

// processGroupsSupported mirrors upstream's USE_PROCESS_GROUPS: closing a
// transport can signal the server's whole process group here.
const processGroupsSupported = true

// configureProcessGroup puts the server in a session of its own, and with it a
// process group (upstream detached: USE_PROCESS_GROUPS, which libuv implements
// with setsid()). The server has no controlling terminal, so it cannot read
// pier's terminal or take its foreground.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// killProcessGroup signals the whole process group. A non-positive pid is
// refused: syscall.Kill(-0, sig) would signal pier's OWN process group, so the
// guard matches processgroup_windows.go rather than relying on callers.
func killProcessGroup(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, sig)
}

// killProcess signals one process.
func killProcess(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

// raiseSelf re-raises a signal on this process after the handler is reset.
func raiseSelf(sig syscall.Signal) error {
	return syscall.Kill(os.Getpid(), sig)
}
