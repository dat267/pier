//go:build !windows

package coding

import "os/exec"

// hideHelperWindow is a no-op off Windows.
func hideHelperWindow(cmd *exec.Cmd) {}
