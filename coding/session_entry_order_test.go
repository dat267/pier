package coding

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts. Each entry type is
// built as an object literal that spreads the base members over its own, so the
// key order differs per type and the persisted JSONL is byte-parity (CR-073).
func TestAppendedEntryKeyOrderMatchesReference(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir()})
	userID := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}, Timestamp: 1})
	manager.AppendCustomEntry("note", json.RawMessage(`{"a":1}`))
	manager.AppendCustomMessageEntry("ctx", json.RawMessage(`{"kind":"x"}`), true, json.RawMessage(`{"d":1}`))
	manager.AppendCustomMessageEntry("plain", json.RawMessage(`{"kind":"y"}`), false, nil)
	manager.AppendUsage("turn", "openai", "gpt-5", ai.Usage{Input: 1, Output: 2, TotalTokens: 3}, "")
	manager.BranchWithSummary(userID, "summary", nil, true, nil)
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manager.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}

	byType := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		keys, entryType := objectKeys(t, line)
		if entryType == "custom" || entryType == "custom_message" || entryType == "usage" || entryType == "branch_summary" {
			// Two custom_message rows are expected; keep both.
			byType[entryType] = append(byType[entryType], strings.Join(keys, ","))
		}
	}

	want := map[string][]string{
		"custom": {"type,customType,data,id,parentId,timestamp"},
		"custom_message": {
			"type,customType,content,display,details,id,parentId,timestamp",
			"type,customType,content,display,id,parentId,timestamp",
		},
		"usage":          {"type,id,parentId,timestamp,kind,provider,model,usage"},
		"branch_summary": {"type,id,parentId,timestamp,fromId,summary,fromHook"},
	}
	for entryType, orders := range want {
		got := byType[entryType]
		if len(got) != len(orders) {
			t.Fatalf("%s: %d entries %v, want %d", entryType, len(got), got, len(orders))
		}
		for index := range orders {
			if got[index] != orders[index] {
				t.Errorf("%s entry %d key order = %s, want %s", entryType, index, got[index], orders[index])
			}
		}
	}
}

// objectKeys returns the top-level keys of one JSONL line in document order.
func objectKeys(t *testing.T, line string) ([]string, string) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(line))
	token, err := decoder.Token()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		t.Fatalf("line is not an object: %s", line)
	}
	var keys []string
	entryType := ""
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		key := keyToken.(string)
		keys = append(keys, key)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("value for %s: %v", key, err)
		}
		if key == "type" {
			_ = json.Unmarshal(value, &entryType)
		}
	}
	return keys, entryType
}
