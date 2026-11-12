package coding

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// pi v1.5.2 (b4afdddf), utils/shell.ts killProcessTree and
// core/tools/bash.ts onAbort: cancelling a shell call kills the process tree,
// including a descendant that still holds the inherited stdout pipe. A port
// that only killed the shell would leave the descendant running (it writes the
// marker) and would block on the pipe the descendant still holds.
func TestBashCancelKillsDescendantsHoldingThePipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell spawns a detached process group only on POSIX")
	}
	cwd := t.TempDir()
	marker := filepath.Join(cwd, "descendant-ran")
	cancel := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		close(cancel)
	}()
	start := time.Now()
	_, err := CreateLocalBashOperations("", nil).Exec(context.Background(),
		"(sleep 0.6; echo late > "+marker+") & sleep 30", cwd, BashExecOptions{Signal: cancel})
	elapsed := time.Since(start)
	if err == nil || err.Error() != "aborted" {
		t.Fatalf("err = %v, want aborted", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("cancel took %s, want the call to return without waiting on the pipe", elapsed)
	}
	// The descendant would write 0.4s after the cancel; it must not survive.
	time.Sleep(800 * time.Millisecond)
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("descendant survived the cancel and wrote its marker")
	}
}
