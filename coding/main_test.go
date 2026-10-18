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
	// Pin the home directory too: skill discovery reads the user-level
	// ~/.agents/skills, and a developer or CI image that has one must not leak
	// skills into these tests (tests that need a home call setHomeDir).
	home, homeErr := os.MkdirTemp("", "pier-test-home-")
	if homeErr == nil {
		_ = os.Setenv("HOME", home)
		_ = os.Setenv("USERPROFILE", home)
	}
	code := m.Run()
	if err == nil {
		_ = os.RemoveAll(dir)
	}
	if homeErr == nil {
		_ = os.RemoveAll(home)
	}
	os.Exit(code)
}
