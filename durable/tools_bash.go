package durable

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of the durable bash tool (tools/bash.ts).

// MaxTimeoutSeconds is the longest timeout the shell tool accepts.
const MaxTimeoutSeconds = 2_147_483_647.0 / 1000.0

// BashToolInput is one bash tool call.
type BashToolInput struct {
	Command string   `json:"command"`
	Timeout *float64 `json:"timeout,omitempty"`
}

// BashExecution is what a prepare hook may adjust.
type BashExecution struct {
	Command    string
	Cwd        string
	Env        map[string]string
	InheritEnv bool
}

// BashPrepare adjusts the execution before it runs.
type BashPrepare func(execution BashExecution, api ToolExecutionApi, ctx chord.Context) error

// BashToolOptions configure the bash tool.
type BashToolOptions struct {
	CommandPrefix string
	Prepare       BashPrepare
}

// ValidateTimeout rejects a non-finite, non-positive or too-large timeout.
func ValidateTimeout(timeout *float64) error {
	if timeout == nil {
		return nil
	}
	value := *timeout
	if value != value || value <= 0 {
		return fmt.Errorf("Invalid timeout: must be a finite number of seconds")
	}
	if value > MaxTimeoutSeconds {
		return fmt.Errorf("Invalid timeout: maximum is %v seconds", MaxTimeoutSeconds)
	}
	return nil
}

// CreateBashTool builds the bash tool: it runs a command through the
// environment's shell, streaming output and spilling past the limits.
func CreateBashTool(options *BashToolOptions) ToolRegistration {
	limits := OutputLimits{Retain: RetainTail}
	return ToolRegistration{
		Tool: ai.Tool{
			Name:        "bash",
			Description: fmt.Sprintf("Execute a bash command in the current working directory. Returns combined stdout and stderr. Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.", DefaultMaxLines, DefaultMaxBytes/1024),
			Parameters:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Bash command to execute"},"timeout":{"type":"number","description":"Timeout in seconds (optional, no default timeout)"}},"required":["command"]}`),
		},
		OutputLimits: &limits,
		Execute: func(args chord.JsonValue, api ToolExecutionApi, ctx chord.Context) (ToolExecutionResult, error) {
			var input BashToolInput
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
			execution := BashExecution{Command: input.Command, Cwd: env.Cwd(), Env: map[string]string{}, InheritEnv: true}
			if options != nil && options.CommandPrefix != "" {
				execution.Command = options.CommandPrefix + "\n" + input.Command
			}
			if options != nil && options.Prepare != nil {
				if err := options.Prepare(execution, api, ctx); err != nil {
					return ToolExecutionResult{}, err
				}
			}
			result, execErr := env.Exec(execution.Command, &ShellExecOptions{
				Cwd: execution.Cwd, Env: execution.Env, InheritEnv: execution.InheritEnv, Timeout: input.Timeout,
				OnOutput: func(text string, _ chord.Context, info ShellOutputInfo) { api.Output([]byte(text), info.Skipped) },
				Spill:    &ShellSpillOptions{AfterBytes: DefaultMaxBytes, AfterLines: DefaultMaxLines},
				Window:   api.OutputWindow(),
			}, ctx)
			spillPath := result.SpillPath
			var executionError *ExecutionError
			if execErr != nil && errors.As(execErr, &executionError) {
				if executionError.SpillPath != "" {
					spillPath = executionError.SpillPath
				}
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
					case "aborted":
						if aborted {
							return ToolExecutionResult{}, execErr
						}
						return ToolExecutionResult{}, fmt.Errorf("Command aborted")
					case "timeout":
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
