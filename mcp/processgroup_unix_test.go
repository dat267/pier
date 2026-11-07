//go:build unix

package mcp

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// A stdio server runs in its own session, with no controlling terminal.
// Upstream spawns it with `detached: USE_PROCESS_GROUPS` (pi v1.5.0 f9a26365,
// packages/mcp/src/transports/stdio.ts), which libuv implements with setsid().
// With only a new process group the server kept pier's terminal: a server that
// prompts on /dev/tty was stopped by SIGTTIN, and a job-control shell in its
// launcher could take the terminal's foreground and leave pier in the
// background. The group kill still reaches the tree: the session leader leads
// its group.
func TestStdioServerRunsInItsOwnSession(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = killProcessGroup(pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()
	if sid, err := unix.Getsid(pid); err != nil || sid != pid {
		t.Fatalf("session = %d (err %v), want the server to lead its own session (%d)", sid, err, pid)
	}
	if pgid, err := unix.Getpgid(pid); err != nil || pgid != pid {
		t.Fatalf("process group = %d (err %v), want %d", pgid, err, pid)
	}
}
