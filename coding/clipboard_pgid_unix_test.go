//go:build unix

package coding

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// Upstream v1.4.0 f8ab362cc (#11179): clipboard helpers run in their own
// process group, so suspending pi with Ctrl+Z does not stop the background
// clipboard owner that keeps serving the selection (wl-copy, xclip, xsel).
func TestClipboardCommandsRunOutsidePiProcessGroup(t *testing.T) {
	for _, name := range []string{"sh", "ps"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is not installed", name)
		}
	}
	output := runClipboardCommand(context.Background(), "sh", []string{"-c", "ps -o pgid= -p $$"}, clipboardCommandOptions{})
	if output == nil {
		t.Fatal("clipboard command did not run")
	}
	childGroup := strings.TrimSpace(string(output))
	ownGroup := strconv.Itoa(syscall.Getpgrp())
	if childGroup == "" || childGroup == ownGroup {
		t.Fatalf("clipboard helper process group %q, pi's is %q", childGroup, ownGroup)
	}
}
