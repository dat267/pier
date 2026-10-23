//go:build !unix

package coding

import "os/exec"

// detachClipboardCommand is a no-op where processes have no group of their own
// (Windows), matching upstream's `detached: process.platform !== "win32"`.
func detachClipboardCommand(cmd *exec.Cmd) {}
