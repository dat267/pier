package coding

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
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
