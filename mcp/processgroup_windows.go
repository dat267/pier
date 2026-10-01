//go:build windows

package mcp

import "os/exec"

// configureProcessGroup leaves the child in this process group; Windows
// termination goes through taskkill /T /F.
func configureProcessGroup(cmd *exec.Cmd) {}
