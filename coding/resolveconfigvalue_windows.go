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
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
