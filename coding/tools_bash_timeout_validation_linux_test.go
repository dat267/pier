//go:build linux

package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// zombieChildren counts this process's exited-but-unreaped children.
func zombieChildren(t *testing.T) int {
	t.Helper()
	self := strconv.Itoa(os.Getpid())
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Skipf("no /proc: %v", err)
	}
	zombies := 0
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// The command name may contain spaces, so parse after its closing paren.
		text := string(data)
		close := strings.LastIndexByte(text, ')')
		if close < 0 || close+2 >= len(text) {
			continue
		}
		fields := strings.Fields(text[close+2:])
		if len(fields) < 2 || fields[1] != self {
			continue
		}
		if strings.HasPrefix(fields[0], "Z") {
			zombies++
		}
	}
	return zombies
}

// TestBashRejectedTimeoutDoesNotSpawn pins upstream's validation order: pi
// v1.5.0 core/tools/bash.ts resolves the timeout in createLocalShellOperations'
// exec (resolveTimeoutMs, the first statement) before it resolves the shell
// config, checks the working directory or spawns, so a rejected timeout means
// the command never ran. A spawn followed by an early return leaves the child
// neither waited for nor killed: it is never reaped, so every rejected timeout
// leaks one zombie process.
func TestBashRejectedTimeoutDoesNotSpawn(t *testing.T) {
	requirePosixShell(t)
	dir := t.TempDir()
	before := zombieChildren(t)
	tool := CreateBashTool(dir, nil)
	for range 10 {
		_, err := tool.Execute("c", json.RawMessage(`{"command":"echo hi","timeout":0}`), context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "Invalid timeout") {
			t.Fatalf("err = %v", err)
		}
	}
	// The shell exits at once when it is spawned, so a leaked child reaches the
	// zombie state quickly; poll rather than assume the scheduler ran it.
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if leaked := zombieChildren(t) - before; leaked > 0 {
			t.Fatalf("%d unreaped child process(es) after 10 rejected timeouts; the command must not be spawned", leaked)
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestBashTimeoutValidationPrecedesOtherInputChecks pins where the validation
// sits in upstream's order: resolveTimeoutMs runs before the working-directory
// check and before the shell is resolved, so a rejected timeout is the reported
// error even when another input is also invalid. The control keeps the
// surrounding checks observable: a valid timeout still reports the missing
// directory and the missing shell.
func TestBashTimeoutValidationPrecedesOtherInputChecks(t *testing.T) {
	requirePosixShell(t)
	dir := t.TempDir()

	missingDir := CreateBashTool(filepath.Join(dir, "gone"), nil)
	if _, err := missingDir.Execute("c", json.RawMessage(`{"command":"echo hi","timeout":0}`), context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "Invalid timeout") {
		t.Fatalf("missing cwd + rejected timeout err = %v", err)
	}
	if _, err := missingDir.Execute("c", json.RawMessage(`{"command":"echo hi"}`), context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "Working directory does not exist") {
		t.Fatalf("missing cwd err = %v", err)
	}

	missingShell := CreateBashTool(dir, &BashToolOptions{ShellPath: filepath.Join(dir, "no-shell")})
	if _, err := missingShell.Execute("c", json.RawMessage(`{"command":"echo hi","timeout":-1}`), context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "Invalid timeout") {
		t.Fatalf("missing shell + rejected timeout err = %v", err)
	}
	if _, err := missingShell.Execute("c", json.RawMessage(`{"command":"echo hi"}`), context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "Custom shell path not found") {
		t.Fatalf("missing shell err = %v", err)
	}
}
