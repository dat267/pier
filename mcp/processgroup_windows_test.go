//go:build windows

package mcp

import (
	"os/exec"
	"testing"
)

// stdio.ts spawns the server with windowsHide: true, so a Go child must not
// flash a console window either.
func TestConfigureProcessGroupHidesTheConsoleWindow(t *testing.T) {
	cmd := &exec.Cmd{}
	configureProcessGroup(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v; want HideWindow", cmd.SysProcAttr)
	}
}
