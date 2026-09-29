package coding

import (
	"fmt"

	"github.com/dat267/pier/agent"
)

// Port of core/tools/index.ts: the tool registry and the tool-set
// constructors.

// ToolName names one built-in tool.
type ToolName = string

// Built-in tool names.
const (
	ToolNameRead       ToolName = "read"
	ToolNameBash       ToolName = "bash"
	ToolNamePowerShell ToolName = "powershell"
	ToolNameEdit       ToolName = "edit"
	ToolNameWrite      ToolName = "write"
	ToolNameGrep       ToolName = "grep"
	ToolNameFind       ToolName = "find"
	ToolNameLS         ToolName = "ls"
)

// AllToolNames are every built-in tool name.
var AllToolNames = []ToolName{
	ToolNameRead,
	ToolNameBash,
	ToolNamePowerShell,
	ToolNameEdit,
	ToolNameWrite,
	ToolNameGrep,
	ToolNameFind,
	ToolNameLS,
}

// IsToolName reports whether a name is a built-in tool.
func IsToolName(name string) bool {
	for _, candidate := range AllToolNames {
		if candidate == name {
			return true
		}
	}
	return false
}

// ToolsOptions carry the per-tool options for the tool-set constructors.
//
// write/edit/grep/find/ls only accept an `operations` override upstream, which
// exists to delegate execution to extensions or remote systems; extension
// mechanics are out of scope for this port, so those tools take no options.
type ToolsOptions struct {
	Read       *ReadToolOptions
	Bash       *BashToolOptions
	PowerShell *BashToolOptions
	// Sandbox confines shell commands and gates write/edit targets for the
	// session's active mode. Nil means no policy.
	Sandbox *Sandbox
}

// withSandboxBashOptions copies the shell options and attaches the sandbox, so
// every shell variant (bash, powershell) carries the policy.
func withSandboxBashOptions(options *BashToolOptions, sandbox *Sandbox) *BashToolOptions {
	if sandbox == nil {
		return options
	}
	copied := BashToolOptions{}
	if options != nil {
		copied = *options
	}
	copied.Sandbox = sandbox
	return &copied
}

// CreateTool builds one built-in tool by name (upstream createTool).
func CreateTool(toolName ToolName, cwd string, options *ToolsOptions) (agent.AgentTool, error) {
	if options == nil {
		options = &ToolsOptions{}
	}
	switch toolName {
	case ToolNameRead:
		return CreateReadTool(cwd, options.Read), nil
	case ToolNameBash:
		return CreateBashTool(cwd, withSandboxBashOptions(options.Bash, options.Sandbox)), nil
	case ToolNamePowerShell:
		return CreatePowerShellTool(cwd, withSandboxBashOptions(options.PowerShell, options.Sandbox)), nil
	case ToolNameEdit:
		return CreateEditTool(cwd, options.Sandbox), nil
	case ToolNameWrite:
		return CreateWriteTool(cwd, options.Sandbox), nil
	case ToolNameGrep:
		return CreateGrepTool(cwd), nil
	case ToolNameFind:
		return CreateFindTool(cwd), nil
	case ToolNameLS:
		return CreateLsTool(cwd), nil
	default:
		return agent.AgentTool{}, fmt.Errorf("Unknown tool name: %s", toolName)
	}
}

// CreateCodingTools is the default write-capable tool set (upstream
// createCodingTools).
func CreateCodingTools(cwd string, options *ToolsOptions) []agent.AgentTool {
	if options == nil {
		options = &ToolsOptions{}
	}
	return []agent.AgentTool{
		CreateReadTool(cwd, options.Read),
		CreateBashTool(cwd, withSandboxBashOptions(options.Bash, options.Sandbox)),
		CreateEditTool(cwd, options.Sandbox),
		CreateWriteTool(cwd, options.Sandbox),
	}
}

// CreateReadOnlyTools is the read-only tool set (upstream
// createReadOnlyTools).
func CreateReadOnlyTools(cwd string, options *ToolsOptions) []agent.AgentTool {
	if options == nil {
		options = &ToolsOptions{}
	}
	return []agent.AgentTool{
		CreateReadTool(cwd, options.Read),
		CreateGrepTool(cwd),
		CreateFindTool(cwd),
		CreateLsTool(cwd),
	}
}

// CreateAllTools builds every built-in tool keyed by name (upstream
// createAllTools).
func CreateAllTools(cwd string, options *ToolsOptions) map[ToolName]agent.AgentTool {
	if options == nil {
		options = &ToolsOptions{}
	}
	powerShellOptions := options.PowerShell
	if powerShellOptions == nil {
		powerShellOptions = options.Bash
	}
	powerShellOptions = withSandboxBashOptions(powerShellOptions, options.Sandbox)
	return map[ToolName]agent.AgentTool{
		ToolNameRead:       CreateReadTool(cwd, options.Read),
		ToolNameBash:       CreateBashTool(cwd, withSandboxBashOptions(options.Bash, options.Sandbox)),
		ToolNamePowerShell: CreatePowerShellTool(cwd, powerShellOptions),
		ToolNameEdit:       CreateEditTool(cwd, options.Sandbox),
		ToolNameWrite:      CreateWriteTool(cwd, options.Sandbox),
		ToolNameGrep:       CreateGrepTool(cwd),
		ToolNameFind:       CreateFindTool(cwd),
		ToolNameLS:         CreateLsTool(cwd),
	}
}
