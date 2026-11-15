//go:build !windows

package mcp

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// groupAlive reports whether any process in the child's process group exists.
func groupAlive(pid int) bool {
	return syscall.Kill(-pid, 0) == nil
}

// Upstream's close() runs the shutdown sequence whenever the host has process
// groups, even for a server that already exited: "a server that already exited
// can leave children holding its pipes; the group cleanup below still has to
// run for them" (packages/mcp/src/transports/stdio.ts). The port returned early
// on an exited child, so a server that forked a background process left it
// running.
func TestStdioCloseKillsAGroupWhoseLeaderExited(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not available")
	}
	transport := NewStdioTransport(StdioTransportOptions{
		Command: "sh",
		Args:    []string{"-c", "sleep 30 & exit 0"},
	})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid := transport.PID()
	if pid <= 0 {
		t.Fatal("no child pid")
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = transport.Close(context.Background())
	})

	// The leader exits immediately; the background sleep keeps the group (and
	// the server's stdout pipe) alive.
	select {
	case <-transport.exitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not exit")
	}
	if !groupAlive(pid) {
		t.Skip("the background process already exited; nothing to observe")
	}

	if err := transport.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for groupAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatal("the server's process group survived Close")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
