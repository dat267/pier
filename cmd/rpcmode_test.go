package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

// pi v1.3.0 rpc-mode.ts unwinds the host on SIGTERM/SIGHUP instead of
// allowing the default process termination to bypass accepted-save cleanup.
func TestRPCSignalUnwinds(t *testing.T) {
	if os.Getenv("PIER_RPC_SIGNAL_CHILD") == "1" {
		code := func() int {
			defer func() { _ = os.WriteFile(os.Getenv("PIER_RPC_UNWOUND"), []byte("unwound"), 0o600) }()
			args := coding.ParseArgs([]string{"--mode", "rpc", "--offline", "--no-session", "--no-tools", "--no-mcp"})
			err := run("pier", args)
			var status interface{ ExitCode() int }
			if errors.As(err, &status) {
				return status.ExitCode()
			}
			if err != nil {
				return 1
			}
			return 0
		}()
		os.Exit(code)
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals")
	}
	isolatedAgentDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PIER_RPC_SIGNAL_CHILD", "1")
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "unwound")
			t.Setenv("PIER_RPC_UNWOUND", marker)
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.run=^TestRPCSignalUnwinds$")
			command.Dir = t.TempDir()
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = command.Process.Kill() }()
			if _, err := io.WriteString(input, "{\"type\":\"get_state\"}\n"); err != nil {
				t.Fatal(err)
			}
			if _, err := bufio.NewReader(output).ReadBytes('\n'); err != nil {
				t.Fatal(err)
			}
			if err := command.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			err = command.Wait()
			want := 143
			if sig == syscall.SIGHUP {
				want = 129
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != want {
				t.Fatalf("exit = %v, want %d", err, want)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "unwound" {
				t.Fatalf("run did not unwind: %q, %v", data, err)
			}
		})
	}
}

func TestRPCRejectsInteractiveStartupInputs(t *testing.T) {
	isolatedAgentDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Chdir(t.TempDir())
	previous := selectResumeSession
	called := false
	selectResumeSession = func(cwd, sessionDir string, settings *coding.SettingsManager) (string, bool) {
		called = true
		return "", false
	}
	defer func() { selectResumeSession = previous }()
	for _, tc := range []struct{ argument, message string }{
		{"--resume", "RPC mode requires --continue or --session instead of the interactive --resume picker"},
		{"@missing", "@file arguments are not supported in RPC mode"},
	} {
		t.Run(tc.argument, func(t *testing.T) {
			args := coding.ParseArgs([]string{"--mode", "rpc", "--offline", "--no-mcp", tc.argument})
			err := run("pier", args)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("error = %v, want %q", err, tc.message)
			}
			if called {
				t.Fatal("RPC opened the interactive session picker")
			}
		})
	}
}

func TestRPCGlobalSessionDoesNotPrompt(t *testing.T) {
	isolatedAgentDir(t)
	source := makeSessionFile(t, t.TempDir())
	previous := promptConfirm
	called := false
	promptConfirm = func(message string) bool { called = true; return false }
	defer func() { promptConfirm = previous }()
	_, err := resumeSession(&coding.Args{Mode: coding.CLIModeRPC, Session: strPtr(source.GetSessionID())}, t.TempDir(), coding.GetAgentDir(), nil)
	if called || err == nil || !strings.Contains(err.Error(), "use --fork or a session file path") {
		t.Fatalf("prompted = %v, error = %v", called, err)
	}
}

func TestRPCSessionIDWarningDoesNotPolluteStdout(t *testing.T) {
	t.Setenv("PIER_RPC_SESSION_ID", "12345678-1234-4234-8234-123456789abc")
	TestRPCStartsHeadlessly(t)
}

// pi v1.3.0 main.ts selects runRpcMode before piped-stdin handling. RPC
// starts without credentials or a terminal and stdout contains only JSONL.
func TestRPCStartsHeadlessly(t *testing.T) {
	if os.Getenv("PIER_RPC_TEST_CHILD") == "1" {
		os.Args = []string{"pier", "--mode", "rpc", "--offline", "--no-mcp", "--no-session", "--no-tools"}
		if id := os.Getenv("PIER_RPC_SESSION_ID"); id != "" {
			os.Args = append(os.Args, "--session-id", id)
		}
		Execute()
		os.Exit(0)
	}
	isolatedAgentDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PIER_RPC_TEST_CHILD", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestRPCStartsHeadlessly$")
	command.Dir = t.TempDir()
	command.Stdin = strings.NewReader("{\"id\":\"state\",\"type\":\"get_state\"}\n")
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("RPC startup failed: %v\n%s", err, stderr.String())
	}
	var response struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Data    struct {
			SessionID   string `json:"sessionId"`
			IsStreaming bool   `json:"isStreaming"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("stdout is not one JSON response: %q: %v", output, err)
	}
	if response.ID != "state" || response.Type != "response" || response.Command != "get_state" || !response.Success || response.Data.SessionID == "" || response.Data.IsStreaming {
		t.Fatalf("unexpected RPC response: %s", output)
	}
}

// pi v1.3.0 rpc-mode.ts new_session rebuilds the runtime session through the
// host factory. The CLI supplies that factory, so a real process reverts to a
// fresh session id.
func TestRPCNewSessionCLI(t *testing.T) {
	if os.Getenv("PIER_RPC_NEW_SESSION_CHILD") == "1" {
		os.Args = []string{"pier", "--mode", "rpc", "--offline", "--no-mcp", "--no-session", "--no-tools"}
		Execute()
		os.Exit(0)
	}
	isolatedAgentDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PIER_RPC_NEW_SESSION_CHILD", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestRPCNewSessionCLI$")
	command.Dir = t.TempDir()
	command.Stdin = strings.NewReader(
		"{\"id\":\"before\",\"type\":\"get_state\"}\n" +
			"{\"id\":\"new\",\"type\":\"new_session\"}\n" +
			"{\"id\":\"after\",\"type\":\"get_state\"}\n")
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("RPC new_session failed: %v\n%s", err, stderr.String())
	}
	type response struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Success bool   `json:"success"`
		Data    struct {
			SessionID string `json:"sessionId"`
			Cancelled bool   `json:"cancelled"`
		} `json:"data"`
	}
	byID := map[string]response{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		var record response
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("stdout is not JSON: %q: %v", line, err)
		}
		if record.Type == "response" {
			byID[record.ID] = record
		}
	}
	if len(byID) != 3 {
		t.Fatalf("responses = %+v", byID)
	}
	if !byID["new"].Success || byID["new"].Data.Cancelled {
		t.Fatalf("new_session = %+v", byID["new"])
	}
	before, after := byID["before"].Data.SessionID, byID["after"].Data.SessionID
	if before == "" || after == "" || before == after {
		t.Fatalf("session ids before=%q after=%q", before, after)
	}
}

// pi v1.3.0 rpc-mode.ts export_html runs the session's HTML export through the
// host renderer and responds with the resolved path. The CLI supplies the
// interactive renderer, so a real process writes a real file.
func TestRPCExportHTMLCLI(t *testing.T) {
	if os.Getenv("PIER_RPC_EXPORT_CHILD") == "1" {
		os.Args = []string{"pier", "--mode", "rpc", "--offline", "--no-mcp", "--no-tools", "--session", os.Getenv("PIER_RPC_EXPORT_SESSION")}
		Execute()
		os.Exit(0)
	}
	isolatedAgentDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	dir := t.TempDir()
	source := makeSessionFile(t, dir)
	output := filepath.Join(t.TempDir(), "rpc-export.html")
	t.Setenv("PIER_RPC_EXPORT_CHILD", "1")
	t.Setenv("PIER_RPC_EXPORT_SESSION", source.GetSessionFile())
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestRPCExportHTMLCLI$")
	command.Dir = dir
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill() }()
	if _, err := io.WriteString(input, `{"id":"e","type":"export_html","outputPath":`+strconv.Quote(output)+"}\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v\n%s", err, stderr.String())
	}
	var response struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			Path string `json:"path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("stdout is not one JSON response: %q: %v", line, err)
	}
	if response.ID != "e" || response.Type != "response" || response.Command != "export_html" || !response.Success {
		t.Fatalf("response = %s (%s)", line, response.Error)
	}
	if response.Data.Path != output {
		t.Fatalf("path = %q want %q", response.Data.Path, output)
	}
	document, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("export file: %v", err)
	}
	if !strings.Contains(string(document), `id="session-data"`) {
		t.Fatalf("export document missing session data: %d bytes", len(document))
	}
	_ = input.Close()
	_ = command.Wait()
}
