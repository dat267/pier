package coding

import (
	"strings"
	"testing"
)

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
