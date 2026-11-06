package coding

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream session-manager.ts appendCompaction records the current system
// message as a full message (`{ ...systemMessage, timestamp }`, so role first)
// and always writes `fromHook` (false for a pi-run compaction). Pier marshaled
// the bare SystemMessage, which has no role field, and dropped a false
// fromHook, so the persisted entry differed from the reference and a reader
// that switches on role saw no system message.
func TestAppendCompactionPersistsTheReferenceEntryShape(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), nil)
	manager.NewSession(nil)
	preamble := "You are a coding assistant"
	manager.AppendMessage(&ai.SystemMessage{
		Sections: map[string]*string{"preamble": &preamble},
	})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	manager.AppendCompaction("summary", "", 10, nil, false, nil)
	path := manager.GetSessionFile()
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var compaction map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(string(encoded)), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry["type"] == "compaction" {
			compaction = entry
		}
	}
	if compaction == nil {
		t.Fatalf("no compaction entry in %s", path)
	}
	fromHook, ok := compaction["fromHook"]
	if !ok || fromHook != false {
		t.Fatalf("fromHook = %v (present %t), want false", fromHook, ok)
	}
	systemMessage, ok := compaction["systemMessage"].(map[string]any)
	if !ok {
		t.Fatalf("systemMessage = %v", compaction["systemMessage"])
	}
	if systemMessage["role"] != "system" {
		t.Fatalf("systemMessage role = %v, want system", systemMessage["role"])
	}
	if systemMessage["sections"] == nil {
		t.Fatalf("systemMessage lost its sections: %v", systemMessage)
	}
}

// The reference's branchWithSummary also writes fromHook for a pi-run summary
// (false), so the persisted branch_summary entry keeps the flag.
func TestBranchSummaryPersistsFromHookFalse(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), nil)
	manager.NewSession(nil)
	leaf := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	manager.BranchWithSummary(leaf, "summary", nil, false, nil)
	path := manager.GetSessionFile()
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(encoded)), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["type"] != "branch_summary" {
			continue
		}
		if value, ok := entry["fromHook"]; !ok || value != false {
			t.Fatalf("fromHook = %v (present %t), want false", value, ok)
		}
		return
	}
	t.Fatalf("no branch_summary entry in %s", path)
}
