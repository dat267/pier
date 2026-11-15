package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.5.2 (b4afdddf) core/session-manager.ts branchWithSummary takes
// `branchFromId: string | null` and writes it straight into `parentId` and
// `this.leafId`, so branching to the root persists `"parentId":null`, not "".
// A "" parent also stayed the leaf, so every later append inherited it.
func TestBranchSummaryToRootPersistsNullParent(t *testing.T) {
	dir := t.TempDir()
	manager := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	summaryID := manager.BranchWithSummary("", "summary", nil, false, nil)
	if summaryID == "" {
		t.Fatal("branch summary not appended")
	}
	if entry := manager.GetEntry(summaryID); entry == nil || entry.ParentID != nil {
		t.Fatalf("summary parent = %v, want nil", entry)
	}
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "after"}})

	raw, err := os.ReadFile(filepath.Join(manager.GetSessionDir(), filepath.Base(manager.GetSessionFile())))
	if err != nil {
		t.Fatal(err)
	}
	// Only the summary itself is a root: appendEntry makes it the new leaf, so
	// the next message is its child (upstream does the same).
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		if string(entry["type"]) != `"branch_summary"` {
			continue
		}
		found = true
		if string(entry["parentId"]) != "null" {
			t.Fatalf("persisted parentId = %s in %q, want null", entry["parentId"], line)
		}
	}
	if !found {
		t.Fatal("branch_summary line not persisted")
	}
}
