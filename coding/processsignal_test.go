//go:build unix

package coding

import (
	"syscall"
	"testing"
)

// pi v1.5.2 (b4afdddf) modes/interactive/external-editor.ts:46 interpolates the exit
// value Node's `close` event provides: `code ?? signal`, where `signal` is a
// NodeJS.Signals name such as "SIGUSR1". rpc-client's exit report uses the same names.
// The port mapped 13 signals and fell back to Go's prose for the rest, so a child killed
// by SIGUSR1 printed "user defined signal 1" and one killed by SIGTSTP printed
// "stopped (tty output)".
func TestProcessSignalNameUsesNodeNames(t *testing.T) {
	for _, tc := range []struct {
		signal syscall.Signal
		want   string
	}{
		{syscall.SIGHUP, "SIGHUP"},
		{syscall.SIGINT, "SIGINT"},
		{syscall.SIGKILL, "SIGKILL"},
		{syscall.SIGTERM, "SIGTERM"},
		{syscall.SIGUSR1, "SIGUSR1"},
		{syscall.SIGUSR2, "SIGUSR2"},
		{syscall.SIGSTOP, "SIGSTOP"},
		{syscall.SIGTSTP, "SIGTSTP"},
		{syscall.SIGCONT, "SIGCONT"},
		{syscall.SIGWINCH, "SIGWINCH"},
		{syscall.SIGCHLD, "SIGCHLD"},
	} {
		if got := ProcessSignalName(tc.signal); got != tc.want {
			t.Errorf("ProcessSignalName(%d) = %q, want %q", int(tc.signal), got, tc.want)
		}
	}
}
