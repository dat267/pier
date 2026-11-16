//go:build !windows

package coding

import "os/exec"

// hideConfigCommandWindow is a no-op: no platform here opens a console window
// for a child process.
func hideConfigCommandWindow(_ *exec.Cmd) {}
