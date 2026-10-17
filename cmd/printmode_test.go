package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

// TestResolvePrintMode covers v1.2.0 main.ts resolveAppMode precedence.
func TestResolvePrintMode(t *testing.T) {
	for _, tc := range []struct {
		name                string
		args                coding.Args
		stdinTTY, stdoutTTY bool
		want                bool
	}{
		{"interactive", coding.Args{}, true, true, false},
		{"piped stdin", coding.Args{}, false, true, true},
		{"redirected stdout", coding.Args{}, true, false, true},
		{"both piped", coding.Args{}, false, false, true},
		{"explicit print", coding.Args{Print: true}, true, true, true},
		{"json", coding.Args{Mode: coding.CLIModeJSON}, true, true, true},
		{"rpc stays rpc", coding.Args{Mode: coding.CLIModeRPC, Print: true}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePrintMode(&tc.args, tc.stdinTTY, tc.stdoutTTY); got != tc.want {
				t.Errorf("headless = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPrintFailureReturnsThroughCommandCleanup(t *testing.T) {
	// v1.3.0 print-mode.ts disposes its host before exiting. run must return
	// the reported status so its MCP/settings/queue defers can execute too.
	if os.Getenv("PIER_PRINT_FAILURE_CHILD") == "1" {
		t.Setenv("OPENAI_API_KEY", "")
		args := coding.ParseArgs([]string{"--offline", "--no-mcp", "--no-session", "--no-tools", "--approve", "--model", "openai/gpt-4o", "-p", "fail without credentials"})
		err := run("pier", args)
		var status interface{ ExitCode() int }
		if !errors.Is(err, errAlreadyReported) || !errors.As(err, &status) || status.ExitCode() != 1 {
			t.Fatalf("print failure = %v, want already-reported exit 1", err)
		}
		return
	}
	isolatedAgentDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PIER_PRINT_FAILURE_CHILD", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestPrintFailureReturnsThroughCommandCleanup$")
	command.Dir = t.TempDir()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("print failure exited before returning through cleanup: %v\n%s", err, output)
	}
}

// TestNonTTYStartsPrintMode follows v1.2.0 main.ts resolveAppMode: a piped
// invocation without -p must finish headlessly rather than start a renderer.
func TestNonTTYStartsPrintMode(t *testing.T) {
	if os.Getenv("PIER_PRINT_MODE_TEST_CHILD") == "1" {
		args := coding.ParseArgs([]string{"--offline", "--no-mcp", "--no-session", "--no-tools", "--approve"})
		if err := run("pier", args); err != nil {
			t.Fatal(err)
		}
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	isolatedAgentDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PIER_PRINT_MODE_TEST_CHILD", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestNonTTYStartsPrintMode$")
	command.Dir = t.TempDir()
	command.Stdin = strings.NewReader("")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("piped invocation did not finish in print mode: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "Press ctrl+o") || strings.Contains(string(output), "\x1b[?25l") {
		t.Fatalf("piped invocation started an interactive renderer:\n%s", output)
	}
}
