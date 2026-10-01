//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the server in its own process group.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
