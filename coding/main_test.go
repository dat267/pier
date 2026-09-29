package coding

import (
	"os"
	"testing"
)

// TestMain lets the sandbox end-to-end tests re-exec this test binary as the
// `pier __sandbox-exec` launcher. The bash tool wraps its argv with
// os.Executable(), which under `go test` is this binary, so the hidden
// subcommand must be handled before the testing flag parser sees it.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == SandboxLauncherSubcommand {
		os.Exit(RunSandboxLauncher(os.Args[1:]))
	}
	os.Exit(m.Run())
}
