package durable

import (
	"context"
	"reflect"
	"testing"
)

// Port of the PowerShell tool's direct-argv fallback from packages/durable/src/tools/bash.ts at pi v1.1.0 commit 68c22123b.
func TestPowerShellToolReportsSpawnAndExitFailures(t *testing.T) {
	base, _ := newToolTestEnv(t)
	unavailable := &argvCaptureEnv{testToolEnv: base, installed: map[string]bool{}}
	if _, err := CreatePowerShellTool(nil).Execute(JsonObject{"command": "1"}, &fakeToolApi{env: unavailable}, context.Background()); err == nil || err.Error() != "spawn powershell ENOENT" {
		t.Fatalf("missing PowerShell error = %v", err)
	}

	nonzero := &argvCaptureEnv{testToolEnv: base, installed: map[string]bool{"pwsh": true}, exitCode: 3}
	api := &fakeToolApi{env: nonzero}
	if _, err := CreatePowerShellTool(nil).Execute(JsonObject{"command": "exit 3"}, api, context.Background()); err == nil || err.Error() != "Command exited with code 3" {
		t.Fatalf("PowerShell exit error = %v", err)
	}
	if api.output.String() != "héllo\n" {
		t.Fatalf("output before failure = %q", api.output.String())
	}
}

// Port of the PowerShell argv arguments and fallback from packages/durable/src/tools/bash.ts at pi v1.1.0 commit 68c22123b.
func TestPowerShellToolUsesArgvAndFallsBackToAvailableProgram(t *testing.T) {
	base, _ := newToolTestEnv(t)
	env := &argvCaptureEnv{testToolEnv: base, installed: map[string]bool{"powershell": true}}
	api := &fakeToolApi{env: env}
	command := "Write-Output 'héllo'"
	result, err := CreatePowerShellTool(nil).Execute(JsonObject{"command": command}, api, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != nil {
		t.Fatalf("tool content = %+v", result.Content)
	}
	if got := api.output.String(); got != "héllo\n" {
		t.Fatalf("tool output = %q", got)
	}
	wantScript := "try { [Console]::OutputEncoding=[System.Text.Encoding]::UTF8 } catch {}\n" + command
	want := [][]string{
		{"pwsh", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", wantScript},
		{"powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", wantScript},
	}
	if !reflect.DeepEqual(env.commands, want) {
		t.Fatalf("argv commands = %#v, want %#v", env.commands, want)
	}
}

type argvCaptureEnv struct {
	*testToolEnv
	installed map[string]bool
	commands  [][]string
	exitCode  int
}

func (e *argvCaptureEnv) ExecArgv(argv []string, options *ShellExecOptions, ctx context.Context) (ShellExecResult, error) {
	e.commands = append(e.commands, append([]string(nil), argv...))
	if !e.installed[argv[0]] {
		return ShellExecResult{}, &ExecutionError{Code: ExecutionErrorSpawn, Message: "spawn " + argv[0] + " ENOENT"}
	}
	if options.OnOutput != nil {
		options.OnOutput("héllo\n", ctx, ShellOutputInfo{Stream: "stdout"})
	}
	return ShellExecResult{ExitCode: e.exitCode}, nil
}

var _ ExecutionEnv = (*argvCaptureEnv)(nil)
