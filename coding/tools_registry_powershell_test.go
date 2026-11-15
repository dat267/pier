package coding

import "testing"

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
