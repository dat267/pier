//go:build !windows

package mcp

import (
	"os"
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the server in a session of its own, and with it a
// process group (upstream detached: USE_PROCESS_GROUPS, which libuv implements
// with setsid()). The server has no controlling terminal, so it cannot read
// pier's terminal or take its foreground.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// killProcessGroup signals the whole process group.
func killProcessGroup(pid int, sig syscall.Signal) error {
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
