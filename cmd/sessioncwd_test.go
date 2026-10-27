package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// main.ts at pi v1.0.2 passes process.cwd() to SessionManager.continueRecent.
// Node returns the physical cwd, even when the shell's PWD names a symlink.
func TestContinueCLIUsesPhysicalWorkingDirectory(t *testing.T) {
	isolatedAgentDir(t)
	physical, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logical := filepath.Join(t.TempDir(), "Books")
	if err := os.Symlink(physical, logical); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	source := makeSessionFile(t, physical)
	ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestContinueCLIHelper$")
	command.Dir = logical
	command.Env = append(os.Environ(), "PWD="+logical, "PIER_TEST_CONTINUE_CWD_HELPER=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("continue CLI: %v: %s", err, output)
	}
	for line := range strings.SplitSeq(string(output), "\n") {
		var header struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal([]byte(line), &header) == nil && header.Type == "session" {
			if header.ID != source.GetSessionID() {
				t.Fatalf("continued session %s, want existing physical-cwd session %s", header.ID, source.GetSessionID())
			}
			return
		}
	}
	t.Fatalf("continue CLI emitted no session header: %s", output)
}

func TestContinueCLIHelper(t *testing.T) {
	if os.Getenv("PIER_TEST_CONTINUE_CWD_HELPER") != "1" {
		return
	}
	os.Args = []string{"pier", "--offline", "--no-mcp", "--continue", "--mode", "json"}
	Execute()
}
