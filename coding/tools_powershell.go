package coding

import (
	"path"
	"strings"

	"github.com/dat267/pier/agent"
)

// Port of core/tools/powershell.ts.

// utf8OutputPrefix forces UTF-8 console output inside PowerShell.
const utf8OutputPrefix = "try { [Console]::OutputEncoding=[System.Text.Encoding]::UTF8 } catch {}\n"

// windowsPowerShellExitSuffix repairs Windows PowerShell 5.1's exit code
// (pi v1.5.2 b4afdddf core/tools/powershell.ts, #8842). It turns redirected
// native stderr into error records and clears $?, so
// `git push 2>&1 | Select-Object -Last 3` exits 1 although git exited 0.
// PowerShell 7 does not. Exit 0 when the last failure is only such a record and
// the native command exited 0; otherwise keep the -Command rule: 0 when the last
// statement succeeded, 1 when it failed. The lone `;` keeps an unfinished last
// statement, such as `Get-ChildItem |`, a parse error instead of continuing into
// the suffix and running.
const windowsPowerShellExitSuffix = "\n;\nif ($?) { exit 0 }\n" +
	"if (($LASTEXITCODE -eq 0) -and (\"$($Error[0].FullyQualifiedErrorId)\" -like \"NativeCommandError*\")) { exit 0 }\n" +
	"exit 1"

// windowsPowerShellExitSuffixFor returns the suffix to append to command for the
// resolved shell: only Windows PowerShell 5.1 needs it, and a command ending in a
// backtick would continue into the suffix instead of ending.
func windowsPowerShellExitSuffixFor(shellPath, command string) string {
	// win32.basename: a Windows path is split on either separator, whatever the
	// host running the port.
	normalized := strings.ReplaceAll(shellPath, "\\", "/")
	if strings.ToLower(path.Base(normalized)) != "powershell.exe" {
		return ""
	}
	if strings.HasSuffix(command, "`") {
		return ""
	}
	return windowsPowerShellExitSuffix
}

// PowerShellToolSystemPromptContribution is the powershell prompt
// contribution (upstream powershellToolSystemPromptContribution).
var PowerShellToolSystemPromptContribution = struct {
	Snippet    string
	Guidelines []string
}{
	Snippet:    "Execute PowerShell commands",
	Guidelines: []string{"You can inspect PI_* environment variables for current model and session details."},
}

// PowerShellShellToolConfig is the PowerShell variant of the shell tool, the
// Go analogue of upstream's createLocalPowerShellOperations: the resolved shell
// config plus the UTF-8 command prefix.
var PowerShellShellToolConfig = ShellToolConfig{
	Name:             "powershell",
	Label:            "powershell",
	ShellName:        "PowerShell",
	Prompt:           "PS>",
	PromptSnippet:    PowerShellToolSystemPromptContribution.Snippet,
	PromptGuidelines: PowerShellToolSystemPromptContribution.Guidelines,
	TempFilePrefix:   "pi-powershell",
	ResolveShell: func(shellPath string) (ShellConfig, error) {
		return GetPowerShellConfig()
	},
	TransformCommand: func(command string) string {
		transformed := utf8OutputPrefix + command
		// A failed resolution reports the Windows-only error when the tool
		// resolves the shell, before any command runs.
		if shellConfig, err := GetPowerShellConfig(); err == nil {
			transformed += windowsPowerShellExitSuffixFor(shellConfig.Shell, command)
		}
		return transformed
	},
}

// CreatePowerShellTool builds the PowerShell tool (upstream
// createPowerShellTool). Shell resolution happens at exec time, so creating the
// tool off Windows succeeds and the execution fails with the Windows-only
// error, matching upstream.
func CreatePowerShellTool(cwd string, options *BashToolOptions) agent.AgentTool {
	// The powershell variant ignores the bash shellPath override: it always
	// resolves PowerShell (upstream createLocalPowerShellOperations).
	localized := BashToolOptions{}
	if options != nil {
		localized = *options
		localized.ShellPath = ""
	}
	return CreateShellTool(cwd, PowerShellShellToolConfig, &localized)
}
