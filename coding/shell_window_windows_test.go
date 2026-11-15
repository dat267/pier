//go:build windows

package coding

import (
	"context"
	"testing"
)

// Reference: pi v1.5.2 utils/shell.ts runs the where lookup with
// `windowsHide: true`, and GetShellConfig does it once per shell command.
func TestShellLookupHidesTheConsoleWindow(t *testing.T) {
	cmd := shellLookupCommand(context.Background(), "where", "bash.exe")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v; want HideWindow", cmd.SysProcAttr)
	}
}
