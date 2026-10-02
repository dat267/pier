//go:build windows

package mcp

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// configureProcessGroup leaves the child in this process group; Windows
// termination goes through taskkill /T /F.
func configureProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup force-kills the process tree (Windows has no process
// groups to signal).
func killProcessGroup(pid int, _ syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	return exec.Command("taskkill", "/pid", strconv.Itoa(pid), "/T", "/F").Run()
}

// killProcess kills one process.
func killProcess(pid int, _ syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

// raiseSelf kills this process (Windows has no signal re-raise).
func raiseSelf(_ syscall.Signal) error {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return process.Kill()
}
