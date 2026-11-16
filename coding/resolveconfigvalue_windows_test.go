//go:build windows

package coding

import (
	"os/exec"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) core/resolve-config-value.ts spawns the
// configured shell with `windowsHide: true`; the default-shell fallback mirrors
// execSync, which does not pass it.
func TestHideConfigCommandWindow(t *testing.T) {
	cmd := exec.Command("cmd.exe")
	hideConfigCommandWindow(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v; want HideWindow", cmd.SysProcAttr)
	}
}
