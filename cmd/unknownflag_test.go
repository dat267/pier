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

// Upstream prints diagnostics with chalk, so a redirected stderr carries no
// escape codes (core/agent-session-services.ts reports through main.ts's
// chalk.red). FORCE_COLOR overrides that, TERM=dumb disables it, and plain
// stderr means the message starts with the error prefix.
func TestDiagnosticColorFollowsColorSupport(t *testing.T) {
	cases := []struct {
		name     string
		env      []string
		wantANSI bool
	}{
		{name: "redirected", env: []string{"TERM=xterm-256color"}, wantANSI: false},
		{name: "forced", env: []string{"TERM=xterm-256color", "FORCE_COLOR=1"}, wantANSI: true},
		{name: "forced off", env: []string{"TERM=xterm-256color", "FORCE_COLOR=0"}, wantANSI: false},
		{name: "dumb terminal", env: []string{"TERM=dumb"}, wantANSI: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			isolatedAgentDir(t)
			ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUnknownFlagCLIHelper$")
			// A clean environment keeps the developer's CI and terminal
			// variables out of the decision.
			command.Env = append([]string{
				"PIER_TEST_UNKNOWN_FLAG_ARGS=--offline --bogus",
				"HOME=" + os.Getenv("HOME"),
			}, testCase.env...)
			output, err := command.CombinedOutput()
			if exit := exitCodeOf(err); exit != 1 {
				t.Fatalf("exit = %d (%v), want 1; output: %q", exit, err, output)
			}
			if got := strings.ContainsAny(string(output), "\x1b"); got != testCase.wantANSI {
				t.Fatalf("ansi = %t, want %t: %q", got, testCase.wantANSI, output)
			}
			if !strings.Contains(string(output), "Error: Unknown option: --bogus") {
				t.Fatalf("missing error line: %q", output)
			}
		})
	}
}
