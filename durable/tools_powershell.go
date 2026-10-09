package durable

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// PowerShellToolInput is one PowerShell tool call.
type PowerShellToolInput struct {
	Command string   `json:"command"`
	Timeout *float64 `json:"timeout,omitempty"`
}

// PowerShellToolOptions configure the PowerShell tool.
type PowerShellToolOptions struct {
	// CommandPrefix runs before each command.
	CommandPrefix string
	// Prepare may adjust command, cwd or environment.
	Prepare BashPrepare
	// Programs are tried in order; nil defaults to pwsh then powershell.
	Programs []string
}

const powerShellUTF8Prefix = "try { [Console]::OutputEncoding=[System.Text.Encoding]::UTF8 } catch {}"

var powerShellArgvPrefix = []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command"}

// CreatePowerShellTool builds a tool that runs PowerShell directly through argv.
func CreatePowerShellTool(options *PowerShellToolOptions) ToolRegistration {
	programs := []string{"pwsh", "powershell"}
	if options != nil && options.Programs != nil {
		programs = options.Programs
	}
	return ToolRegistration{
		Tool: ai.Tool{
			Name:        "powershell",
			Description: fmt.Sprintf("Execute a PowerShell command in the current working directory. Returns combined stdout and stderr. Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.", DefaultMaxLines, DefaultMaxBytes/1024),
			Parameters:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"PowerShell command to execute"},"timeout":{"type":"number","description":"Timeout in seconds (optional, no default timeout)"}},"required":["command"]}`),
		},
		OutputLimits: &OutputLimits{Retain: RetainTail},
		Execute: func(args chord.JsonValue, api ToolExecutionApi, ctx chord.Context) (ToolExecutionResult, error) {
			var input PowerShellToolInput
			if err := decodeArgs(args, &input); err != nil {
				return ToolExecutionResult{}, err
			}
			if err := ValidateTimeout(input.Timeout); err != nil {
				return ToolExecutionResult{}, err
			}
			env, err := RequireEnv(api)
			if err != nil {
				return ToolExecutionResult{}, err
			}
			command := input.Command
			if options != nil && options.CommandPrefix != "" {
				command = options.CommandPrefix + "\n" + command
			}
			execution := BashExecution{Command: command, Cwd: env.Cwd(), Env: map[string]string{}, InheritEnv: true}
			if options != nil && options.Prepare != nil {
				if err := options.Prepare(execution, api, ctx); err != nil {
					return ToolExecutionResult{}, err
				}
			}
			script := powerShellUTF8Prefix + "\n" + execution.Command
			shellOptions := &ShellExecOptions{
				Cwd: execution.Cwd, Env: execution.Env, InheritEnv: execution.InheritEnv,
				Timeout: input.Timeout, Window: api.OutputWindow(),
				OnOutput: func(text string, _ chord.Context, info ShellOutputInfo) {
					api.Output([]byte(text), info.Skipped)
				},
				Spill: &ShellSpillOptions{AfterBytes: DefaultMaxBytes, AfterLines: DefaultMaxLines},
			}
			var result ShellExecResult
			var execErr error
			attempted := false
			for _, program := range programs {
				argv := make([]string, 0, 1+len(powerShellArgvPrefix)+1)
				argv = append(argv, program)
				argv = append(argv, powerShellArgvPrefix...)
				argv = append(argv, script)
				result, execErr = env.ExecArgv(argv, shellOptions, ctx)
				attempted = true
				var executionError *ExecutionError
				if execErr == nil || !errors.As(execErr, &executionError) || executionError.Code != ExecutionErrorSpawn {
					break
				}
			}
			if !attempted {
				return ToolExecutionResult{}, fmt.Errorf("No command to run")
			}
			spillPath := result.SpillPath
			var executionError *ExecutionError
			if execErr != nil && errors.As(execErr, &executionError) && executionError.SpillPath != "" {
				spillPath = executionError.SpillPath
			}
			if spillPath != "" {
				api.Diagnostic(ToolDiagnostic{
					Severity: DiagnosticInfo, Code: stringPointer("full_output"),
					Message: "Full output: " + spillPath,
				})
			}
			if execErr != nil {
				if errors.As(execErr, &executionError) {
					aborted := ctx != nil && ctx.Err() != nil
					switch executionError.Code {
					case ExecutionErrorAborted:
						if aborted {
							return ToolExecutionResult{}, execErr
						}
						return ToolExecutionResult{}, fmt.Errorf("Command aborted")
					case ExecutionErrorTimeout:
						timeout := 0.0
						if input.Timeout != nil {
							timeout = *input.Timeout
						}
						return ToolExecutionResult{}, fmt.Errorf("Command timed out after %v seconds", timeout)
					}
				}
				return ToolExecutionResult{}, execErr
			}
			if result.ExitCode != 0 {
				return ToolExecutionResult{}, fmt.Errorf("Command exited with code %d", result.ExitCode)
			}
			return ToolExecutionResult{}, nil
		},
	}
}
