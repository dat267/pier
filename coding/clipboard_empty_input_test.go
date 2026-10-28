package coding

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
)

// pi v1.4.0 (4a080252), utils/clipboard-command.ts selects ignored stdout
// by input !== undefined, not by truthiness. An empty copy is still a writer,
// so its output must not be collected or subjected to the reader's byte cap.
func TestClipboardEmptyCopyStillIgnoresWriterOutput(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not installed")
	}
	for _, text := range []string{"", "control"} {
		t.Run(fmt.Sprintf("text=%q", text), func(t *testing.T) {
			getenv := func(name string) string {
				if name == "WAYLAND_DISPLAY" {
					return "fixture"
				}
				return ""
			}
			run := func(ctx context.Context, _ string, _ []string, options clipboardCommandOptions) []byte {
				// A tiny reader cap makes accidental stdout collection fail
				// without needing a large fixture or a daemonized real clipboard.
				options.maxBufferBytes = 4
				return runClipboardCommand(ctx, sh, []string{"-c", "printf diagnostic-output"}, options)
			}
			if err := copyTextToClipboardWith(context.Background(), "linux", getenv, run, nil, nil, text); err != nil {
				t.Fatalf("copy failed because writer output was treated as clipboard input: %v", err)
			}
		})
	}
}
