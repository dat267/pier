package durable

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Port of the shell half of env/node.ts.

func newTestShell(t *testing.T) (*OSShell, string) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	dir := t.TempDir()
	shell, err := NewOSShell(dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return shell, dir
}

func requireShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the shell tests run the unix shell resolution")
	}
}

func TestOSShellOutputAndExitCode(t *testing.T) {
	requireShell(t)
	shell, _ := newTestShell(t)
	ctx := context.Background()
	var output strings.Builder
	result, err := shell.Exec("printf 'one\\ntwo\\n'", &ShellExecOptions{
		OnOutput: func(text string, _ context.Context) { output.WriteString(text) },
	}, ctx)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if output.String() != "one\ntwo\n" {
		t.Fatalf("output = %q", output.String())
	}
	// A failing command reports its exit code without an error.
	result, err = shell.Exec("exit 3", nil, ctx)
	if err != nil || result.ExitCode != 3 {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestOSShellTimeoutValidation(t *testing.T) {
	requireShell(t)
	shell, _ := newTestShell(t)
	ctx := context.Background()
	zero := 0.0
	if _, err := shell.Exec("true", &ShellExecOptions{Timeout: &zero}, ctx); err == nil {
		t.Fatal("zero timeout must fail")
	} else {
		var executionError *ExecutionError
		if !errors.As(err, &executionError) || executionError.Code != ExecutionErrorTimeout ||
			!strings.Contains(executionError.Message, "finite number of seconds") {
			t.Fatalf("err = %+v", err)
		}
	}
	huge := 3_000_000.0
	if _, err := shell.Exec("true", &ShellExecOptions{Timeout: &huge}, ctx); err == nil ||
		!strings.Contains(err.Error(), "maximum is 2147483") {
		t.Fatalf("err = %v", err)
	}
}

func TestOSShellTimeoutKillsTheCommand(t *testing.T) {
	requireShell(t)
	shell, _ := newTestShell(t)
	timeout := 0.2
	start := time.Now()
	_, err := shell.Exec("sleep 30", &ShellExecOptions{Timeout: &timeout}, context.Background())
	if time.Since(start) > 10*time.Second {
		t.Fatal("timeout did not kill the command")
	}
	var executionError *ExecutionError
	if !errors.As(err, &executionError) || executionError.Code != ExecutionErrorTimeout ||
		executionError.Message != "timeout:0.2" {
		t.Fatalf("err = %+v", err)
	}
}

func TestOSShellCancellation(t *testing.T) {
	requireShell(t)
	shell, _ := newTestShell(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := shell.Exec("sleep 30", nil, ctx)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		var executionError *ExecutionError
		if !errors.As(err, &executionError) || executionError.Code != ExecutionErrorAborted {
			t.Fatalf("err = %+v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation did not stop the command")
	}
}

func TestOSShellSpillsLongOutput(t *testing.T) {
	requireShell(t)
	shell, _ := newTestShell(t)
	result, err := shell.Exec("printf 'a\\nb\\nc\\nd\\ne\\n'", &ShellExecOptions{
		Spill: &ShellSpillOptions{AfterBytes: 1_000_000, AfterLines: 2},
	}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.SpillPath == "" {
		t.Fatal("output over the line threshold must spill")
	}
	content, err := os.ReadFile(result.SpillPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "a\nb\nc\nd\ne\n" {
		t.Fatalf("spill = %q", content)
	}
	if err := shell.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.SpillPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cleanup must remove the spill file")
	}
	// Output below both thresholds does not spill.
	result, err = shell.Exec("echo short", &ShellExecOptions{
		Spill: &ShellSpillOptions{AfterBytes: 1_000_000, AfterLines: 1000},
	}, context.Background())
	if err != nil || result.SpillPath != "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestOSShellWorkingDirectoryAndEnv(t *testing.T) {
	requireShell(t)
	shell, dir := newTestShell(t)
	ctx := context.Background()
	marker := filepath.Join(dir, "marker.txt")
	result, err := shell.Exec("touch marker.txt", &ShellExecOptions{Cwd: dir}, ctx)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("command did not run in the requested directory")
	}
	// A missing working directory is a spawn error.
	if _, err := shell.Exec("true", &ShellExecOptions{Cwd: filepath.Join(dir, "missing")}, ctx); err == nil ||
		!strings.Contains(err.Error(), "Working directory does not exist") {
		t.Fatalf("err = %v", err)
	}
	// inheritEnv false passes only the explicit environment.
	var output strings.Builder
	if _, err := shell.Exec("printf %s \"$PIER_SHELL_TEST\"", &ShellExecOptions{
		InheritEnv: false,
		Env:        map[string]string{"PIER_SHELL_TEST": "explicit"},
		OnOutput:   func(text string, _ context.Context) { output.WriteString(text) },
	}, ctx); err != nil {
		t.Fatal(err)
	}
	if output.String() != "explicit" {
		t.Fatalf("output = %q", output.String())
	}
}

func TestOSShellCallbackError(t *testing.T) {
	requireShell(t)
	shell, _ := newTestShell(t)
	_, err := shell.Exec("echo boom", &ShellExecOptions{
		OnOutput: func(string, context.Context) { panic("listener exploded") },
	}, context.Background())
	var executionError *ExecutionError
	if !errors.As(err, &executionError) || executionError.Code != ExecutionErrorCallback ||
		!strings.Contains(executionError.Message, "listener exploded") {
		t.Fatalf("err = %+v", err)
	}
}
