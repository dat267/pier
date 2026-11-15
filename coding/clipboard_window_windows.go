//go:build windows

package coding

import (
	"os/exec"
	"syscall"
)

// hideHelperWindow hides the console window a clipboard helper would otherwise
// create, matching the reference's spawn option `windowsHide: true`
// (v1.5.2 clipboard-command.ts). The browser launcher shares
// detachHelperCommand but not this call: upstream's open-browser spawn does not
// pass the option.
func hideHelperWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}
