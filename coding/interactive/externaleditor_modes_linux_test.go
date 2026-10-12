//go:build linux

package interactive

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// Upstream external-editor.ts uses writeFileSync's default mode in a 0700 temp directory.
func TestExternalEditorTempFileUsesNodeDefaultMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	t.Setenv("TMPDIR", t.TempDir())

	previous := externalEditorRunner
	defer func() { externalEditorRunner = previous }()
	var mode os.FileMode
	externalEditorRunner = func(command *exec.Cmd) error {
		path := command.Args[len(command.Args)-1]
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		mode = info.Mode().Perm()
		return nil
	}
	result := EditInExternalEditor(ExternalEditorOptions{Command: "editor", Content: "prompt"})
	if result.Status != "complete" {
		t.Fatalf("editor result = %+v", result)
	}
	if mode != 0o666 {
		t.Fatalf("editor temp file mode = %#o; want Node default %#o", mode, 0o666)
	}
}
