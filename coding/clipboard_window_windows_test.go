//go:build windows

package coding

import (
	"os/exec"
	"testing"
)

// Reference: pi v1.5.2 clipboard-command.ts spawns clipboard helpers with
// `windowsHide: true`, so no console window appears when pier has none.
func TestClipboardHelperHidesTheConsoleWindow(t *testing.T) {
	cmd := exec.Command("clip")
	hideHelperWindow(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v; want HideWindow", cmd.SysProcAttr)
	}
}
