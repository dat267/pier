//go:build !windows

package mcp

import (
	"os"
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the server in its own process group.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
