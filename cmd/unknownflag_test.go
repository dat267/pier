package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Upstream main.ts hands every unclaimed unknown flag to agent-session-services,
// which turns it into a fatal "Unknown option: --name" diagnostic
// (core/agent-session-services.ts at f9a26365). With extensions out of scope
// (D41) nothing can claim one, so the CLI must reject it instead of ignoring a
// misspelled flag.
func TestUnknownLongFlagFailsTheRun(t *testing.T) {
	isolatedAgentDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUnknownFlagCLIHelper$")
	command.Env = append(os.Environ(), "PIER_TEST_UNKNOWN_FLAG_ARGS=--offline --bogus")
	output, err := command.CombinedOutput()
	if exit := exitCodeOf(err); exit != 1 {
		t.Fatalf("exit = %d (%v), want 1; output: %s", exit, err, output)
	}
	if !strings.Contains(string(output), "Error: Unknown option: --bogus") {
		t.Fatalf("missing unknown-option error: %s", output)
	}
}

// Upstream --version returns before the unknown-flag diagnostics are produced,
// so a bad flag does not stop a version query.
func TestUnknownLongFlagStillPrintsVersion(t *testing.T) {
	isolatedAgentDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUnknownFlagCLIHelper$")
	command.Env = append(os.Environ(), "PIER_TEST_UNKNOWN_FLAG_ARGS=--bogus --version")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("version run failed: %v: %s", err, output)
	}
	if strings.Contains(string(output), "Unknown option") {
		t.Fatalf("version run reported the flag: %s", output)
	}
	if strings.TrimSpace(string(output)) == "" {
		t.Fatal("version run printed nothing")
	}
}

// exitCodeOf reports the process exit code, or -1 when the child failed without
// one.
func exitCodeOf(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if err == nil {
		return 0
	}
	return -1
}

func TestUnknownFlagCLIHelper(t *testing.T) {
	args, ok := os.LookupEnv("PIER_TEST_UNKNOWN_FLAG_ARGS")
	if !ok {
		return
	}
	os.Args = append([]string{"pier"}, strings.Fields(args)...)
	Execute()
}
