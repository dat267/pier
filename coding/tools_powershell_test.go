package coding

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Tests keyed to upstream utils/shell.ts, core/tools/powershell.ts, and
// core/tools/index.ts.

func TestGetShellConfigResolution(t *testing.T) {
	config, err := GetShellConfig("")
	if err != nil {
		t.Fatalf("default shell config: %v", err)
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(strings.ToLower(config.Shell), "bash") {
			t.Fatalf("shell = %s", config.Shell)
		}
	} else if config.Shell != "/bin/bash" && !strings.HasSuffix(config.Shell, "bash") {
		t.Fatalf("shell = %s", config.Shell)
	}
	if len(config.Args) != 1 || config.Args[0] != "-c" {
		t.Fatalf("args = %#v", config.Args)
	}
	if config.CommandTransport != "" {
		t.Fatalf("transport = %s", config.CommandTransport)
	}

	// A missing custom shell path keeps upstream's message.
	if _, err := GetShellConfig("/definitely/missing/bash"); err == nil ||
		!strings.Contains(err.Error(), "Custom shell path not found: /definitely/missing/bash") {
		t.Fatalf("err = %v", err)
	}

	// A legacy WSL bash path switches to stdin transport.
	legacy := `C:\Windows\System32\bash.exe`
	if !isLegacyWslBashPath(legacy) {
		t.Fatalf("legacy WSL path not detected: %s", legacy)
	}
	if isLegacyWslBashPath(`C:\Program Files\Git\bin\bash.exe`) {
		t.Fatal("Git Bash must not use stdin transport")
	}
	// A custom shell path is honored. /bin/sh exists only on Unix, so use a
	// shell the platform actually has.
	customShell := "/bin/sh"
	if runtime.GOOS == "windows" {
		customShell = config.Shell
	}
	config, err = GetShellConfig(customShell)
	if err != nil {
		t.Fatalf("custom shell: %v", err)
	}
	if config.Shell != customShell || config.Args[0] != "-c" {
		t.Fatalf("config = %#v", config)
	}
}

func TestGetPowerShellConfigWindowsOnly(t *testing.T) {
	config, err := GetPowerShellConfig()
	if runtime.GOOS != "windows" {
		if err == nil || err.Error() != "The powershell tool is only available on Windows." {
			t.Fatalf("err = %v", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(config.Args) != len(PowerShellArgs) {
		t.Fatalf("args = %#v", config.Args)
	}
	for index, arg := range PowerShellArgs {
		if config.Args[index] != arg {
			t.Fatalf("args = %#v", config.Args)
		}
	}
}

func TestSanitizeBinaryOutput(t *testing.T) {
	input := "ok\tline\nnext\rnull\x00bell\x07format\uFFF9tail"
	got := SanitizeBinaryOutput(input)
	want := "ok\tline\nnext\rnullbellformattail"
	if got != want {
		t.Fatalf("sanitized = %q, want %q", got, want)
	}
	// Escapes are stripped; DEL (0x7f) is above the control-character range
	// upstream filters, so it survives.
	if got := SanitizeBinaryOutput("\x1b[31mred\x1b[0m\x7f"); got != "[31mred[0m\x7f" {
		t.Fatalf("sanitized = %q", got)
	}
}

func TestPowerShellToolMetadataAndExecFailure(t *testing.T) {
	tool := CreatePowerShellTool(t.TempDir(), nil)
	if tool.Name != "powershell" || tool.Label != "powershell" {
		t.Fatalf("tool = %s/%s", tool.Name, tool.Label)
	}
	if !strings.Contains(tool.Description, "PowerShell") {
		t.Fatalf("description = %s", tool.Description)
	}
	if PowerShellShellToolConfig.TempFilePrefix != "pi-powershell" {
		t.Fatalf("prefix = %s", PowerShellShellToolConfig.TempFilePrefix)
	}
	if PowerShellShellToolConfig.Prompt != "PS>" {
		t.Fatalf("prompt = %s", PowerShellShellToolConfig.Prompt)
	}
	if PowerShellToolSystemPromptContribution.Snippet != "Execute PowerShell commands" {
		t.Fatalf("snippet = %s", PowerShellToolSystemPromptContribution.Snippet)
	}
	if len(PowerShellToolSystemPromptContribution.Guidelines) != 1 ||
		PowerShellToolSystemPromptContribution.Guidelines[0] != "You can inspect PI_* environment variables for current model and session details." {
		t.Fatalf("guidelines = %#v", PowerShellToolSystemPromptContribution.Guidelines)
	}
	// The UTF-8 prefix is prepended to every command.
	if got := PowerShellShellToolConfig.TransformCommand("Get-Date"); got != utf8OutputPrefix+"Get-Date" {
		t.Fatalf("transformed = %q", got)
	}
	// The schema is the shared shell schema.
	var schema map[string]any
	if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	properties, _ := schema["properties"].(map[string]any)
	if _, ok := properties["command"]; !ok {
		t.Fatalf("schema = %s", tool.Parameters)
	}

	// Off Windows, execution fails with the Windows-only error (the tool never
	// falls back to bash).
	if runtime.GOOS != "windows" {
		_, err := tool.Execute(context.Background(), "call-1", json.RawMessage(`{"command":"Get-Date"}`), nil)
		if err == nil || err.Error() != "The powershell tool is only available on Windows." {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestBashToolUsesSharedShellConfig(t *testing.T) {
	tool := CreateBashTool(t.TempDir(), nil)
	if tool.Name != "bash" || tool.Label != "bash" {
		t.Fatalf("tool = %s/%s", tool.Name, tool.Label)
	}
	if BashShellToolConfig.TempFilePrefix != "pi-bash" {
		t.Fatalf("prefix = %s", BashShellToolConfig.TempFilePrefix)
	}
	if BashToolSystemPromptContribution.Snippet != "Execute bash commands (ls, grep, find, etc.)" {
		t.Fatalf("snippet = %s", BashShellToolConfig.PromptSnippet)
	}
	// The shared shell tool still runs commands end to end.
	result, err := tool.Execute(context.Background(), "call-1", json.RawMessage(`{"command":"echo shared-config"}`), nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("no content")
	}
	if text := result.Content[0].(ai.TextContent).Text; !strings.Contains(text, "shared-config") {
		t.Fatalf("output = %q", text)
	}
}

func TestToolRegistry(t *testing.T) {
	if len(AllToolNames) != 8 {
		t.Fatalf("tool names = %#v", AllToolNames)
	}
	expected := []string{"read", "bash", "powershell", "edit", "write", "grep", "find", "ls"}
	for index, name := range expected {
		if AllToolNames[index] != name {
			t.Fatalf("tool names = %#v", AllToolNames)
		}
		if !IsToolName(name) {
			t.Fatalf("IsToolName(%q) = false", name)
		}
	}
	if IsToolName("unknown") {
		t.Fatal("unknown names must not be tools")
	}

	cwd := t.TempDir()
	for _, name := range AllToolNames {
		tool, err := CreateTool(name, cwd, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tool.Name != name {
			t.Fatalf("%s: tool name = %s", name, tool.Name)
		}
	}
	if _, err := CreateTool("unknown", cwd, nil); err == nil ||
		!strings.Contains(err.Error(), "Unknown tool name: unknown") {
		t.Fatalf("err = %v", err)
	}

	coding := CreateCodingTools(cwd, nil)
	if len(coding) != 4 {
		t.Fatalf("coding tools = %d", len(coding))
	}
	for index, name := range []string{"read", "bash", "edit", "write"} {
		if coding[index].Name != name {
			t.Fatalf("coding tools = %s at %d", coding[index].Name, index)
		}
	}
	readOnly := CreateReadOnlyTools(cwd, nil)
	if len(readOnly) != 4 {
		t.Fatalf("read-only tools = %d", len(readOnly))
	}
	for index, name := range []string{"read", "grep", "find", "ls"} {
		if readOnly[index].Name != name {
			t.Fatalf("read-only tools = %s at %d", readOnly[index].Name, index)
		}
	}
	all := CreateAllTools(cwd, nil)
	if len(all) != 8 {
		t.Fatalf("all tools = %d", len(all))
	}
	for _, name := range AllToolNames {
		if all[name].Name != name {
			t.Fatalf("all tools[%s] = %s", name, all[name].Name)
		}
	}
}

// Upstream's regex requires a letter drive (`^[a-z]:...`).
func TestIsLegacyWslBashPathRequiresDriveLetter(t *testing.T) {
	if isLegacyWslBashPath(`1:\windows\system32\bash.exe`) {
		t.Fatal("a non-letter drive should not match")
	}
}

// PowerShell discovery falls back to installation directories when the shell is
// not on PATH. Upstream v1.4.0 61060ddf8: %ProgramFiles%\PowerShell\7\pwsh.exe is
// checked before powershell.exe on PATH, and %SystemRoot%\System32\WindowsPowerShell\v1.0
// last, so an install on another drive without a PATH entry still works.
func TestResolvePowerShellShellOrder(t *testing.T) {
	onDisk := map[string]bool{}
	onPath := map[string]string{}
	exists := func(path string) bool { return onDisk[path] }
	findOnPath := func(executable string) string { return onPath[executable] }
	env := func(key string) string {
		switch key {
		case "ProgramFiles":
			return `C:\Program Files`
		case "SystemRoot":
			return `D:\Win`
		}
		return ""
	}
	pwsh7 := filepath.Join(env("ProgramFiles"), "PowerShell", "7", "pwsh.exe")
	windowsPowerShell := filepath.Join(env("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")

	cases := []struct {
		name  string
		setup func()
		want  string
	}{
		{"pwsh on PATH", func() { onPath["pwsh.exe"] = `C:\bin\pwsh.exe` }, `C:\bin\pwsh.exe`},
		{"pwsh 7 in ProgramFiles", func() { onDisk[pwsh7] = true }, pwsh7},
		{"powershell on PATH", func() { onPath["powershell.exe"] = `C:\bin\powershell.exe` }, `C:\bin\powershell.exe`},
		{"windows powershell in SystemRoot", func() { onDisk[windowsPowerShell] = true }, windowsPowerShell},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			clear(onDisk)
			clear(onPath)
			testCase.setup()
			config, err := resolvePowerShellShell("windows", env, findOnPath, exists)
			if err != nil {
				t.Fatal(err)
			}
			if config.Shell != testCase.want {
				t.Fatalf("shell = %q; want %q", config.Shell, testCase.want)
			}
			if len(config.Args) != len(PowerShellArgs) {
				t.Fatalf("args = %#v", config.Args)
			}
		})
	}

	// Nothing found anywhere, and no environment to fall back on.
	clear(onDisk)
	clear(onPath)
	if _, err := resolvePowerShellShell("windows", env, findOnPath, exists); err == nil ||
		err.Error() != "No PowerShell executable found. Install PowerShell or add powershell.exe/pwsh.exe to PATH." {
		t.Fatalf("err = %v", err)
	}
	// Missing environment variables fall back to the PATH search only.
	emptyEnv := func(string) string { return "" }
	if _, err := resolvePowerShellShell("windows", emptyEnv, findOnPath, exists); err == nil {
		t.Fatal("missing installation directories should fail the same way")
	}
	if _, err := resolvePowerShellShell("linux", env, findOnPath, exists); err == nil ||
		err.Error() != "The powershell tool is only available on Windows." {
		t.Fatalf("err = %v", err)
	}
}

// The legacy WSL bash launcher lives in the configured Windows directory, which
// need not be C:\Windows: upstream v1.4.0 61060ddf8 compares against
// %SystemRoot%\System32 and %SystemRoot%\Sysnative.
func TestIsLegacyWslBashPathHonorsSystemRoot(t *testing.T) {
	t.Setenv("SystemRoot", `D:\Win`)
	for _, path := range []string{`D:\Win\System32\bash.exe`, `D:\WIN\system32\BASH.EXE`, `D:\Win\Sysnative\bash.exe`} {
		if !isLegacyWslBashPath(path) {
			t.Fatalf("%q should be the legacy WSL launcher", path)
		}
	}
	// With the Windows directory known, a different drive's C:\Windows is not it.
	for _, path := range []string{`C:\Windows\System32\bash.exe`, `D:\Win\System32\wsl.exe`, `D:\Win\bash.exe`} {
		if isLegacyWslBashPath(path) {
			t.Fatalf("%q must not be the legacy WSL launcher", path)
		}
	}
	// A Windows directory that is not drive-absolute cannot match either.
	for _, systemRoot := range []string{`\\server\share`, `Win`, `relative\Win`} {
		t.Setenv("SystemRoot", systemRoot)
		if isLegacyWslBashPath(`C:\Windows\System32\bash.exe`) {
			t.Fatalf("SystemRoot %q must not fall back to the conventional path", systemRoot)
		}
	}

	// Without a Windows directory, only the conventional location matches.
	t.Setenv("SystemRoot", "")
	for _, path := range []string{`C:\Windows\System32\bash.exe`, `d:\windows\sysnative\bash.exe`} {
		if !isLegacyWslBashPath(path) {
			t.Fatalf("%q should match the conventional launcher path", path)
		}
	}
	// Any drive letter matches, since the Windows directory is unknown.
	for _, path := range []string{`C:\Windows\System32\bash`, `C:\Windows\bash.exe`, `C:\Windows\System64\bash.exe`} {
		if isLegacyWslBashPath(path) {
			t.Fatalf("%q must not match the conventional launcher path", path)
		}
	}

	// Separators and dot segments normalize like win32 path normalization.
	t.Setenv("SystemRoot", `D:/Win/`)
	for _, path := range []string{`D:\Win\System32\bash.exe`, `D:/Win/System32/bash.exe`, `D:\Win\\System32\bash.exe`, `D:\Win\.\System32\bash.exe`, `D:\Win\other\..\System32\bash.exe`} {
		if !isLegacyWslBashPath(path) {
			t.Fatalf("%q should normalize to the legacy WSL launcher", path)
		}
	}
}

// pi v1.5.2 (b4afdddf) core/tools/powershell.ts WINDOWS_POWERSHELL_EXIT_SUFFIX:
// Windows PowerShell 5.1 reports a failed exit code for a native command whose
// redirected stderr became an error record (#8842), so the suffix retries the
// rule with $LASTEXITCODE and the error id. PowerShell 7 needs no suffix, and a
// command ending in a backtick must not continue into it.
//
// The suffix cannot be reached from a test on other platforms: the shell tool
// refuses to resolve PowerShell off Windows before a command runs. This pins the
// reference's exact text and the conditions instead.
func TestWindowsPowerShellExitSuffix(t *testing.T) {
	if !strings.Contains(windowsPowerShellExitSuffix, "if ($?) { exit 0 }") ||
		!strings.Contains(windowsPowerShellExitSuffix, `"NativeCommandError*"`) ||
		!strings.HasSuffix(windowsPowerShellExitSuffix, "\nexit 1") {
		t.Fatalf("suffix = %q", windowsPowerShellExitSuffix)
	}

	cases := []struct {
		name     string
		shell    string
		command  string
		wantSame bool
	}{
		{name: "windows-powershell", shell: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, command: "git push 2>&1 | Select-Object -Last 3"},
		{name: "relative", shell: "powershell.exe", command: "Get-Date"},
		{name: "mixed-case", shell: `C:\Windows\System32\WindowsPowerShell\v1.0\PowerShell.EXE`, command: "Get-Date"},
		{name: "powershell-7", shell: `C:\Program Files\PowerShell\7\pwsh.exe`, command: "Get-Date", wantSame: true},
		{name: "other-shell", shell: "/bin/bash", command: "Get-Date", wantSame: true},
		{name: "trailing-backtick", shell: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, command: "Write-Output `", wantSame: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			suffix := windowsPowerShellExitSuffixFor(tc.shell, tc.command)
			if tc.wantSame {
				if suffix != "" {
					t.Fatalf("suffix = %q, want none", suffix)
				}
				return
			}
			if suffix != windowsPowerShellExitSuffix {
				t.Fatalf("suffix = %q, want the Windows PowerShell suffix", suffix)
			}
		})
	}
}

// TestAllToolsPowerShellOptionsIgnoreBashOptions pins pi v1.5.2 (b4afdddf)
// core/tools/index.ts createAllTools: the PowerShell tool gets
// `options?.powershell` and nothing else. Pier fell back to the bash options,
// so a caller who configured bash alone had its shell prefix and
// session-environment flags applied to PowerShell too.
func TestAllToolsPowerShellOptionsIgnoreBashOptions(t *testing.T) {
	bashOnly := &ToolsOptions{Bash: &BashToolOptions{CommandPrefix: "leak", ExposeSessionEnvironment: new(false)}}
	if got := allToolsPowerShellOptions(bashOnly); got != nil {
		t.Fatalf("bash-only options reached PowerShell: %+v", got)
	}
	if got := allToolsPowerShellOptions(nil); got != nil {
		t.Fatalf("nil options gave %+v", got)
	}
	powerShell := &BashToolOptions{CommandPrefix: "ps"}
	if got := allToolsPowerShellOptions(&ToolsOptions{Bash: &BashToolOptions{CommandPrefix: "bash"}, PowerShell: powerShell}); got != powerShell {
		t.Fatalf("PowerShell options = %+v, want the powershell entry", got)
	}
}
