//go:build windows

package durable

import (
	"os/exec"
	"strconv"
)

// configureProcessGroup is a no-op: Windows has no process groups here.
func configureProcessGroup(cmd *exec.Cmd) {}

// killProcessTree kills the whole tree with taskkill.
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	taskkill := exec.Command("taskkill", "/F", "/T", "/PID", pid)
	_ = taskkill.Run()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
