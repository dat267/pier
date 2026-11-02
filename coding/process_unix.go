//go:build unix

package coding

import (
	"os/exec"
	"runtime"
	"sync"
	"syscall"
)

// configureDetachedCommand starts the command in its own process group so a
// kill can target the whole tree (upstream's `detached: true`, which it sets on
// every platform but Windows).
func configureDetachedCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTreePlatform kills the process group with SIGKILL, falling back to
// the single process if the group kill fails (upstream killProcessTree, the
// non-win32 branch).
//
// Darwin signals group members individually after releasing the group lock, so
// a killed child can wake its waiting shell and let `a; b` start b during a
// cancellation. Stopping the group first reduces that race; SIGKILL still
// reaches stopped processes. Linux signals the shell before a child's exit can
// be observed, so it needs no stop (pi v1.5.0 shell.ts, #11252).
func killProcessTreePlatform(pid int) {
	if runtime.GOOS == "darwin" {
		_ = syscall.Kill(-pid, syscall.SIGSTOP)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// processTreeGuard holds the process group identity, which outlives the shell:
// a group kill therefore reaches descendants that detached from the tree, unlike
// a pid-based walk (D178).
type processTreeGuard struct {
	pid  int
	once sync.Once
}

// newProcessTreeGuard records the group leader pid (the shell's).
func newProcessTreeGuard(pid int) *processTreeGuard { return &processTreeGuard{pid: pid} }

// Kill signals the group exactly once. sync.Once makes it safe when the abort
// goroutine and the timeout timer fire together.
func (g *processTreeGuard) Kill() {
	if g == nil {
		return
	}
	g.once.Do(func() { killProcessTreePlatform(g.pid) })
}

// Release has nothing to close off unix.
func (g *processTreeGuard) Release() {}
