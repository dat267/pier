//go:build windows

package coding

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// fileSize reads a file's size, failing the test when it is gone.
func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// waitForGrowth blocks until the file grows past before and returns its size.
func waitForGrowth(t *testing.T, path string, before int64) int64 {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if size := fileSize(t, path); size > before {
			return size
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("writer never wrote: the orphan did not start")
	return 0
}

// TestProcessTreeGuardKillsOrphanedGrandchild pins the Windows half of D178: a
// grandchild whose parent has already exited is invisible to `taskkill /T`,
// which walks a live parent-child tree, so the job object is what still reaches
// it. The launcher pauses briefly before starting the writer, which is the order
// the shell tool has too: the guard is created right after the shell starts.
func TestProcessTreeGuardKillsOrphanedGrandchild(t *testing.T) {
	dir := t.TempDir()
	beat := filepath.Join(dir, "beat.txt")
	file, err := os.Create(beat)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	// ping writes a line a second, so the file growing is the writer's
	// heartbeat. The writer inherits this handle through `start /b`, and the
	// intermediate cmd.exe that launched it exits at once, orphaning it.
	launcher := exec.Command("cmd.exe", "/c",
		"ping -n 2 127.0.0.1 >nul & start /b /min cmd.exe /c ping -n 30 127.0.0.1")
	launcher.Stdout = file
	launcher.Stderr = file
	if err := launcher.Start(); err != nil {
		t.Fatal(err)
	}
	guard := newProcessTreeGuard(launcher.Process.Pid)
	defer guard.Release()
	_ = launcher.Wait()

	size := waitForGrowth(t, beat, 0)
	guard.Kill()
	time.Sleep(1500 * time.Millisecond)
	if grew := fileSize(t, beat); grew > size {
		t.Fatalf("orphaned writer survived the job kill: %d -> %d bytes", size, grew)
	}
}

// TestProcessTreeGuardReleaseKeepsBackgroundProcess pins the other half of
// D178: a normal completion must not kill what the command deliberately left
// running (a background server is a supported pattern), so Release closes the
// job handle without terminating it.
func TestProcessTreeGuardReleaseKeepsBackgroundProcess(t *testing.T) {
	dir := t.TempDir()
	beat := filepath.Join(dir, "beat.txt")
	file, err := os.Create(beat)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	launcher := exec.Command("cmd.exe", "/c",
		"ping -n 2 127.0.0.1 >nul & start /b /min cmd.exe /c ping -n 5 127.0.0.1")
	launcher.Stdout = file
	launcher.Stderr = file
	if err := launcher.Start(); err != nil {
		t.Fatal(err)
	}
	guard := newProcessTreeGuard(launcher.Process.Pid)
	_ = launcher.Wait()

	size := waitForGrowth(t, beat, 0)
	guard.Release()
	time.Sleep(1500 * time.Millisecond)
	if grew := fileSize(t, beat); grew <= size {
		t.Fatalf("background writer was killed by Release: %d -> %d bytes", size, grew)
	}
	// The surviving writer still holds the file open. Close the test's own
	// handle and wait for the writer to finish, so the temp directory can be
	// cleaned up.
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if err := os.Remove(beat); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the background writer never exited")
}
