package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// AGENTS.md: tests use temporary agent state, never the real agent directory.
// These child processes build their environment from scratch, so the agent dir
// must be passed explicitly; without it the child reads the developer's real
// ~/.pi/agent, whose session directory can be hundreds of megabytes.
func TestChildProcessesUseTheIsolatedAgentDir(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	want := filepath.Clean(agentDir)

	cases := []struct {
		name   string
		helper string
		env    []string
	}{
		{"run failure", "TestRunFailureCLIHelper", []string{"PIER_TEST_RUN_FAILURE_ARGS=--offline -p hi", "TERM=xterm-256color"}},
		{"unknown flag", "TestUnknownFlagCLIHelper", []string{"PIER_TEST_UNKNOWN_FLAG_ARGS=--offline --bogus"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+testCase.helper+"$")
			command.Env = childAgentEnv(append(append([]string{}, testCase.env...), "PIER_TEST_PRINT_AGENT_DIR=1")...)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("child failed: %v: %s", err, output)
			}
			var got string
			for line := range strings.SplitSeq(string(output), "\n") {
				if value, ok := strings.CutPrefix(line, "AGENTDIR="); ok {
					got = filepath.Clean(value)
				}
			}
			if got != want {
				t.Fatalf("child agent dir = %q, want the isolated %q (it reads the developer's real agent directory otherwise)", got, want)
			}
		})
	}
}
