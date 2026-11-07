//go:build unix

package coding

import (
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A shell command runs in its own session, with no controlling terminal.
// Upstream spawns it with `detached: true` (pi v1.5.0 f9a26365,
// packages/coding-agent/src/core/tools/bash.ts), and libuv implements that
// with setsid(). Pier only set a new process group, so the command kept pier's
// terminal: a prompt that reads /dev/tty (`chezmoi apply` asking whether to
// overwrite a changed file) read from a background group, was stopped by
// SIGTTIN, and the command hung until a timeout or Esc. A job-control shell
// in the command could also take the terminal's foreground with tcsetpgrp;
// killed on a timeout, it never gave it back, and pier's next read stopped pier
// itself ("zsh: suspended (tty input)", with mouse reports echoed as text).
// Without a controlling terminal the open of /dev/tty fails and the command
// ends with that error, and the command cannot change pier's foreground.
func TestDetachedCommandRunsInItsOwnSession(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	configureDetachedCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	guard := newProcessTreeGuard(pid)
	defer func() {
		guard.Kill()
		_ = cmd.Wait()
	}()

	sid, err := unix.Getsid(pid)
	if err != nil {
		t.Fatal(err)
	}
	if sid != pid {
		t.Fatalf("session = %d, want the command to lead its own session (%d)", sid, pid)
	}
	// The group kill still reaches the tree: a session leader leads its group.
	pgid, err := unix.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgid != pid {
		t.Fatalf("process group = %d, want %d", pgid, pid)
	}

	guard.Kill()
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the group kill did not end the command")
	}
}
