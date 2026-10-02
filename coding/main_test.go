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
	// Tests must never write into the developer's real agent directory: a
	// session built with the default agent dir wrote one session shard per
	// temp cwd there (tens of thousands of `--tmp-Test…--` directories), and
	// that is the user's data. Pin it for the whole binary.
	dir, err := os.MkdirTemp("", "pier-test-agent-")
	if err == nil {
		_ = os.Setenv("PI_CODING_AGENT_DIR", dir)
	}
	code := m.Run()
	if err == nil {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
