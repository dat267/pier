//go:build linux

package interactive

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/dat267/pier/coding"
)

// pi v1.4.0 packages/coding-agent/src/modes/interactive/interactive-mode.ts
// writes the user-selected debug export with Node's default file mode.
func TestWriteDebugLogFileUsesPrivateMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(dir, "agent"))
	if err := WriteDebugLogFile("debug output"); err != nil {
		t.Fatal(err)
	}
	debugPath := filepath.Join(dir, "agent", coding.AppName+"-debug.log")
	fileInfo, err := os.Stat(debugPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("debug file mode = %#o; want %#o", got, 0o600)
	}
	if err := os.Chmod(debugPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteDebugLogFile("replacement debug output"); err != nil {
		t.Fatal(err)
	}
	fileInfo, err = os.Stat(debugPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("replaced debug file mode = %#o; want %#o", got, 0o600)
	}
	dirInfo, err := os.Stat(filepath.Dir(debugPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Fatalf("debug directory mode = %#o; want Node default %#o", got, 0o777)
	}
}
