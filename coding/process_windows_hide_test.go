//go:build windows

package coding

import (
	"os/exec"
	"testing"
)

// Reference: pi v1.5.2 spawns the bash tool's shell (core/tools/bash.ts) and
// the configured shell (utils/shell.ts) with `windowsHide: true`.
func TestConfigureDetachedCommandHidesTheConsoleWindow(t *testing.T) {
	cmd := exec.Command("cmd")
	configureDetachedCommand(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v; want HideWindow", cmd.SysProcAttr)
	}
}
