package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	// v1.4.0 print-mode.ts disposes its host before exiting. run must return
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
	ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
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
	ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
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

// TestPrintModeProviderErrorKeepsTerminalControlsOut covers D463: text mode
// prints a failed request's message to stderr, and a provider's HTTP body can
// carry escape sequences. stderr is a diagnostic channel, so it is sanitized;
// stdout carries the model's answer and stays raw because it is data.
func TestPrintModeProviderErrorKeepsTerminalControlsOut(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"hostile", `{"error":{"message":"boom` + "\x1b]0;owned\x07clear\x1b[2J" + `"}}`, "boom"},
		{"clean", `{"error":{"message":"boom"}}`, "boom"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			isolatedAgentDir(t)
			agentDir := os.Getenv("PI_CODING_AGENT_DIR")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, testCase.body)
			}))
			defer server.Close()
			models := fmt.Sprintf(`{"providers":{"stub":{"baseUrl":%q,"api":"openai-completions","apiKey":"k","models":[{"id":"m"}]}}}`, server.URL)
			if err := os.WriteFile(filepath.Join(agentDir, "models.json"), []byte(models), 0o600); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPrintModeProviderErrorHelper$")
			command.Env = childAgentEnv(
				"PIER_TEST_PRINT_PROVIDER_ERROR=1",
				"PI_SKIP_VERSION_CHECK=1",
				"TERM=xterm-256color",
			)
			output, _ := command.CombinedOutput()
			text := string(output)
			if strings.ContainsAny(text, "\x1b\x07") {
				t.Fatalf("provider error text reached the terminal: %q", text)
			}
			if !strings.Contains(text, testCase.want) {
				t.Fatalf("output lost the error text: %q", text)
			}
		})
	}
}

func TestPrintModeProviderErrorHelper(t *testing.T) {
	if os.Getenv("PIER_TEST_PRINT_PROVIDER_ERROR") != "1" {
		return
	}
	os.Args = []string{"pier", "--no-mcp", "--no-session", "--no-tools", "--approve", "--model", "stub/m", "-p", "hi"}
	Execute()
}
