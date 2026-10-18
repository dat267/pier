package interactive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The diagnostics live in <agentDir>/logs, not in the agent dir root: the
// CPU-profile records alone littered a real agent dir with 475 files.
func TestDiagnosticPathsUseTheLogsDirectory(t *testing.T) {
	if got := stallLogPath("/agent"); got != "/agent/logs/pier-stall.log" {
		t.Fatalf("stallLogPath = %q", got)
	}
	if got := inputLatencyLogPath("/agent"); got != "/agent/logs/pier-input-latency.log" {
		t.Fatalf("inputLatencyLogPath = %q", got)
	}
	if stallLogPath("") != "" || inputLatencyLogPath("") != "" {
		t.Fatal("empty agent dir must disable diagnostics")
	}
}

// Writing a CPU profile prunes the oldest ones: the live flood produced 475
// files in one afternoon.
func TestStallProfilesPruneOldest(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 25; i++ {
		path := filepath.Join(dir, fmt.Sprintf("pier-stall-cpu-20261018T150000.%09d.pprof", i))
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pruneStallProfiles(dir, 20)
	entries, err := filepath.Glob(filepath.Join(dir, "*.pprof"))
	if err != nil || len(entries) != 20 {
		t.Fatalf("profiles = %d, want 20", len(entries))
	}
	// The newest survived.
	for _, name := range entries {
		if strings.Contains(name, "000000024.pprof") {
			return
		}
	}
	t.Fatal("newest profile was pruned")
}
