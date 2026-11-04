//go:build unix

package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Reference: pi f9a26365 core/resolve-config-value.ts bounds execSync/spawnSync
// at 10s. A shell's child retaining stdout must not extend that deadline.
func TestConfigCommandTimeoutDoesNotWaitForDescendantPipeEOF(t *testing.T) {
	assertConfigCommandPipeTimeout(t, func(command string) bool {
		_, ok := ResolveConfigValueUncached("!"+command, nil)
		return ok
	})
}

func TestConfiguredShellTimeoutDoesNotWaitForDescendantPipeEOF(t *testing.T) {
	assertConfigCommandPipeTimeout(t, func(command string) bool {
		value, _ := executeWithConfiguredShell(command, nil)
		return value != ""
	})
}

func assertConfigCommandPipeTimeout(t *testing.T, run func(string) bool) {
	t.Helper()
	previous := configCommandTimeout
	configCommandTimeout = 250 * time.Millisecond
	t.Cleanup(func() { configCommandTimeout = previous })
	gate := filepath.Join(t.TempDir(), "gate")
	if err := unix.Mkfifo(gate, 0o600); err != nil {
		t.Fatal(err)
	}
	pipe, err := os.OpenFile(gate, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	done := make(chan bool, 1)
	go func() {
		// The trailing command keeps the shell from exec-ing cat in place.
		done <- run(fmt.Sprintf("cat %q; :", gate))
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("timed-out config command returned a successful value")
		}
	case <-time.After(configCommandTimeout + time.Second):
		// Release the child before failing so no process or waiter leaks.
		pipe.Close()
		<-done
		t.Fatal("config timeout waited for descendant pipe EOF instead of returning")
	}
}
