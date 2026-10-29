//go:build linux

package tui

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// pi v1.4.0 packages/tui/src/terminal.ts appends terminal output to this
// opt-in log with Node's default mode; Pier protects captured output.
func TestTerminalWriteLogUsesPrivateMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	for _, existing := range []bool{false, true} {
		name := "new-file"
		if existing {
			name = "existing-file"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "terminal.log")
			if existing {
				if err := os.WriteFile(path, []byte("old output"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			stdout, err := os.Create(filepath.Join(dir, "stdout"))
			if err != nil {
				t.Fatal(err)
			}
			defer stdout.Close()
			terminal := &ProcessTerminal{stdout: stdout, writeLogPath: path}
			terminal.writeDirect("Bearer secret\n")
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("terminal log mode = %#o, want %#o", got, 0o600)
			}
		})
	}
}
