//go:build windows

package coding

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/sys/windows"
)

// configureDetachedCommand is a no-op on Windows: there is no process group to
// join, so the tree identity comes from the job object in
// newProcessTreeGuard (upstream detaches the shell only off win32).
func configureDetachedCommand(cmd *exec.Cmd) {}

// killProcessTreePlatform kills the process and its children with taskkill,
// resolved under System32 so cleanup does not depend on PATH (upstream
// killProcessTree, the win32 branch). Errors are ignored — the process may
// already be gone — and a failed spawn cannot crash the caller.
func killProcessTreePlatform(pid int) {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	taskkill := filepath.Join(systemRoot, "System32", "taskkill.exe")
	_ = exec.Command(taskkill, "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
}

// processTreeGuard holds the job object that owns the command's whole tree.
//
// D178: `taskkill /T` walks the live parent-child tree by pid, so a descendant
// whose parent already exited is unreachable, and a cancel would leave the work
// running. A job object reaches it: children of a job member join the same job
// automatically and stay in it after the parent exits, so TerminateJobObject
// kills the work the shell started no matter how it detached. The handle is
// created right after the shell starts, before it can spawn anything.
type processTreeGuard struct {
	mu       sync.Mutex
	job      windows.Handle
	pid      int
	assigned bool
	killOnce sync.Once
}

// newProcessTreeGuard creates the job and moves the started shell into it. Any
// step failing (a host that forbids the assignment, an exiting shell) leaves the
// guard unassigned, and Kill falls back to taskkill.
func newProcessTreeGuard(pid int) *processTreeGuard {
	guard := &processTreeGuard{pid: pid}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return guard
	}
	guard.job = job
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return guard
	}
	defer windows.CloseHandle(handle)
	if err := windows.AssignProcessToJobObject(job, handle); err != nil {
		return guard
	}
	guard.assigned = true
	return guard
}

// Kill terminates the tree, through the job when it was assigned and through
// taskkill otherwise.
// terminateJob is the job terminate call, an indirection so the Windows test can
// pin that a released handle is never used (production is windows.TerminateJobObject).
var terminateJob = windows.TerminateJobObject

// Kill terminates the tree, through the job when it was assigned and through
// taskkill otherwise. The lock is held across the terminate so a concurrent
// Release cannot close the job handle mid-call; once Release has run, g.job is
// zero and the taskkill fallback is used instead of a closed handle.
func (g *processTreeGuard) Kill() {
	if g == nil {
		return
	}
	g.killOnce.Do(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.assigned && g.job != 0 {
			if err := terminateJob(g.job, 1); err == nil {
				return
			}
		}
		killProcessTreePlatform(g.pid)
	})
}

// Release closes the job handle without terminating it, so a process the
// command deliberately left running survives a normal completion. It is safe
// after Kill and safe to call twice.
func (g *processTreeGuard) Release() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}
