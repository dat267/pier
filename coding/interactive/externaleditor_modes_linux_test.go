//go:build linux

package interactive

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// Upstream external-editor.ts uses writeFileSync's default mode in a 0700 temp directory.
// external-editor.ts reads edited text with UTF-8 replacement semantics.
func TestExternalEditorReplacesMalformedUTF8(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	previous := externalEditorRunner
	defer func() { externalEditorRunner = previous }()
	externalEditorRunner = func(command *exec.Cmd) error {
		return os.WriteFile(command.Args[len(command.Args)-1], []byte{0xff, 'x'}, 0o600)
	}
	result := EditInExternalEditor(ExternalEditorOptions{Command: "editor", Content: "prompt"})
	if result.Status != "complete" || result.Content != "\uFFFDx" {
		t.Fatalf("editor result = %+v; want replacement-decoded content", result)
	}
}

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

// A non-zero editor exit reports the code and keeps the prompt, like upstream's
// "External editor exited with code N; prompt unchanged".
func TestExternalEditorExitCodeMessage(t *testing.T) {
	stubExternalEditorRunner(t, func(*exec.Cmd) error {
		return exec.Command("sh", "-c", "exit 3").Run()
	})
	result := EditInExternalEditor(ExternalEditorOptions{Command: "my-editor", Content: "prompt"})
	if result.Status != "failed" || result.Message != "External editor exited with code 3; prompt unchanged" {
		t.Fatalf("result = %+v", result)
	}
}

// Upstream external-editor.ts reports a signalled editor by name:
// "External editor exited with SIGTERM; prompt unchanged".
func TestExternalEditorSignalMessage(t *testing.T) {
	stubExternalEditorRunner(t, func(*exec.Cmd) error {
		return exec.Command("sh", "-c", "kill -TERM $$").Run()
	})
	result := EditInExternalEditor(ExternalEditorOptions{Command: "my-editor", Content: "prompt"})
	if result.Status != "failed" || result.Message != "External editor exited with SIGTERM; prompt unchanged" {
		t.Fatalf("result = %+v", result)
	}
}

// Reference: pi v1.6.0 modes/interactive/external-editor.ts reads the temp file
// with `stripBom(readFileSync(filePath, "utf-8"))` inside a `finally` that only
// cleans up, so an editor that deletes the file makes the synchronous readthrow
// out of `editInExternalEditor`. The Go port cannot return that as a panic to
// its caller, so it reports a failed result whose message names the read; D504
// records the difference.
func TestExternalEditorReportsADeletedPromptFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	previous := externalEditorRunner
	defer func() { externalEditorRunner = previous }()
	externalEditorRunner = func(command *exec.Cmd) error {
		return os.Remove(command.Args[len(command.Args)-1])
	}
	result := EditInExternalEditor(ExternalEditorOptions{Command: "editor", Content: "prompt"})
	if result.Status != "failed" {
		t.Fatalf("editor result = %+v; want a failed result", result)
	}
	if !strings.Contains(result.Message, "Failed to read the edited prompt") {
		t.Fatalf("message = %q", result.Message)
	}
}
