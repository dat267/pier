//go:build !linux

package coding

import (
	"fmt"
	"os"
)

// No kernel sandbox backend outside Linux. macOS Seatbelt and the Windows ACL
// restricted-token runner are not ported: the mode model and the footer stay
// identical, but workspace-write is unenforceable, so the default falls back to
// read-only and only an explicit `/permission FA` lifts confinement.

// DetectSandboxBackend reports no backend on non-Linux hosts.
func DetectSandboxBackend() SandboxBackend { return SandboxBackendNone }

func sandboxConfinementArgv(allowlist []string, command []string) ([]string, error) {
	return nil, fmt.Errorf("sandbox: no kernel sandbox backend on this platform")
}

// RunSandboxLauncher is unreachable without a backend.
func RunSandboxLauncher(args []string) int {
	fmt.Fprintln(os.Stderr, "sandbox: no kernel sandbox backend on this platform — command refused")
	return 125
}
