//go:build windows

package coding

import (
	"os/exec"
	"syscall"
)

// hideConfigCommandWindow hides the console window a configured shell would open
// when pier itself has none (a GUI host or a service). Upstream passes
// `windowsHide: true` to this spawnSync call, and not to the execSync fallback the
// default shell uses (pi v1.5.2 core/resolve-config-value.ts).
func hideConfigCommandWindow(cmd *exec.Cmd) {
	// A caller may already have set other SysProcAttr fields (a job object, a
	// process group); overwriting the struct would drop them, so set only the
	// one field, as hideHelperWindow does.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}
