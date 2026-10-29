package coding

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Run the cyclic-file traversal in a child so a pre-fix infinite walk is
// bounded without leaving a spinning goroutine in the test process.
func TestSessionParentCycleDoesNotHang(t *testing.T) {
	if os.Getenv("PIER_TEST_SESSION_CYCLE_CHILD") == "1" {
		manager, err := OpenSession(os.Getenv("PIER_TEST_SESSION_CYCLE_FILE"), "", "")
		if err != nil {
			t.Fatal(err)
		}
		branch := manager.GetBranch("")
		if len(branch) != 2 || branch[0].ID != "m3" || branch[1].ID != "m4" {
			t.Fatalf("cycle branch = %+v, want each entry once in root-to-leaf order", branch)
		}
		entries := manager.GetEntries()
		leafID := "m2"
		path := buildSessionPath(entries, &leafID, nil)
		if len(path) != 2 || path[0].ID != "m1" || path[1].ID != "m2" {
			t.Fatalf("cycle path = %+v, want each entry once in root-to-leaf order", path)
		}
		collected := CollectEntriesForBranchSummary(manager, "m2", "m4")
		if collected.HasAncestor || len(collected.Entries) != 2 {
			t.Fatalf("disconnected-cycle collection = %+v, want two entries without an ancestor", collected)
		}
		return
	}

	path := filepath.Join(t.TempDir(), "cycle.jsonl")
	const content = `{"type":"session","version":3,"id":"s","timestamp":"t","cwd":"/tmp"}` + "\n" +
		`{"type":"message","id":"m1","parentId":"m2","timestamp":"t","message":{"role":"user","content":"x"}}` + "\n" +
		`{"type":"message","id":"m2","parentId":"m1","timestamp":"t","message":{"role":"assistant","content":"y"}}` + "\n" +
		`{"type":"message","id":"m3","parentId":"m4","timestamp":"t","message":{"role":"user","content":"z"}}` + "\n" +
		`{"type":"message","id":"m4","parentId":"m3","timestamp":"t","message":{"role":"assistant","content":"w"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSessionParentCycleDoesNotHang$")
	cmd.Env = append(os.Environ(), "PIER_TEST_SESSION_CYCLE_CHILD=1", "PIER_TEST_SESSION_CYCLE_FILE="+path)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("parent-cycle traversal did not terminate: %s", output)
	}
	if err != nil {
		t.Fatalf("child traversal failed: %v\n%s", err, output)
	}
}
