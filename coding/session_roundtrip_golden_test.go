package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.6.0 (faac5858) packages/coding-agent/src/core/session-manager.ts
// (SessionManager appends, loadEntriesFromFile, JSON.stringify per line). The
// golden records what the pinned code wrote, then what a load followed by a
// stringify rewrite produced. Pier rewrites a session after a branch edit, a
// compaction or a migration, so both must reproduce the reference's bytes
// rather than dropping a field, a line or an unknown member.
// Regenerate: node coding/testdata/session-roundtrip/generate.mjs ./pi > coding/testdata/session-roundtrip/golden.json
func TestSessionFileRoundTripsReferenceBytes(t *testing.T) {
	raw, err := os.ReadFile("testdata/session-roundtrip/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			Name    string `json:"name"`
			Seed    string `json:"seeded"`
			Content string `json:"content"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden.Cases) < 2 {
		t.Fatalf("golden holds %d cases", len(golden.Cases))
	}

	for _, testCase := range golden.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			if !strings.HasSuffix(testCase.Content, "\n") || strings.Count(testCase.Content, "\n") < 2 {
				t.Fatalf("golden holds %d bytes across %d lines", len(testCase.Content), strings.Count(testCase.Content, "\n"))
			}
			seed := testCase.Seed
			if seed == "" {
				seed = testCase.Content
			}
			dir := t.TempDir()
			source := filepath.Join(dir, "source.jsonl")
			if err := os.WriteFile(source, []byte(seed), 0o600); err != nil {
				t.Fatal(err)
			}
			entries, err := LoadEntriesFromFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != strings.Count(seed, "\n") {
				t.Fatalf("loaded %d entries, the file holds %d lines", len(entries), strings.Count(seed, "\n"))
			}
			rewritten := filepath.Join(dir, "rewritten.jsonl")
			if err := writeSessionEntries(rewritten, entries); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(rewritten)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) == testCase.Content {
				return
			}
			gotLines, wantLines := strings.Split(string(got), "\n"), strings.Split(testCase.Content, "\n")
			for index := 0; index < len(wantLines) || index < len(gotLines); index++ {
				var want, have string
				if index < len(wantLines) {
					want = wantLines[index]
				}
				if index < len(gotLines) {
					have = gotLines[index]
				}
				if want != have {
					t.Fatalf("line %d differs:\n got %s\nwant %s", index+1, have, want)
				}
			}
			t.Fatal("rewritten session differs from the reference bytes")
		})
	}

	// The entry-type case has to keep every type present, so a shrunken golden
	// cannot quietly stop covering one.
	types := map[string]bool{}
	source := filepath.Join(t.TempDir(), "types.jsonl")
	if err := os.WriteFile(source, []byte(golden.Cases[0].Content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadEntriesFromFile(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		switch {
		case entry.Header != nil:
			types["session"] = true
		case entry.Entry != nil:
			types[entry.Entry.Type] = true
		}
	}
	for _, entryType := range []string{"session", "message", "thinking_level_change", "model_change", "usage", "session_info", "custom", "custom_message", "context_edit", "label", "compaction"} {
		if !types[entryType] {
			t.Errorf("the %s case covers no %s entry", golden.Cases[0].Name, entryType)
		}
	}
}

// The fixture above proves a load followed by a rewrite reproduces the
// reference's bytes. It never runs the append path, which builds each line from
// a fresh struct rather than re-emitting retained bytes, so the two can drift:
// the reference writes an entry as an object literal, and four of its types
// carry their payload before the base members (see SessionEntry.MarshalJSON).
// This pins the append path's key order for every entry type the fixture
// carries, by comparing against the reference's own line for that type.
func TestSessionAppendMatchesReferenceKeyOrder(t *testing.T) {
	raw, err := os.ReadFile("testdata/session-roundtrip/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}

	// The reference's top-level key order per entry type, and per message role,
	// since the seven message roles are one entry type.
	wantKeys := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSuffix(golden.Cases[0].Content, "\n"), "\n") {
		key := sessionLineKey(line)
		if key == "" {
			continue
		}
		if _, seen := wantKeys[key]; !seen {
			wantKeys[key] = topLevelKeyOrder(line)
		}
	}
	if len(wantKeys) < 15 {
		t.Fatalf("the fixture yielded %d entry shapes, want every type", len(wantKeys))
	}

	// Build one entry of each type through the append path.
	dir := t.TempDir()
	manager := NewSessionManager("/work", &SessionManagerOptions{SessionDir: dir})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}, Timestamp: 1})
	manager.AppendThinkingLevelChange("high")
	manager.AppendModelChange("openai", "gpt-5")
	manager.AppendUsage("compaction", "anthropic", "m", ai.Usage{Input: 1}, "note")
	manager.AppendSessionInfo("name")
	manager.AppendCustomEntry("state", json.RawMessage(`{"a":1}`))
	// details and usage are omitempty, so a nil one drops the key entirely: pass
	// values, as the fixture's own appends did, or this compares a shape the
	// reference never wrote.
	manager.AppendCustomMessageEntry("notice", json.RawMessage(`"plain"`), true, json.RawMessage(`{"level":"warn"}`))
	manager.AppendCompaction("summary", "", 10, json.RawMessage(`{"strategy":"summary"}`), true,
		&ai.Usage{Input: 1, Output: 2, TotalTokens: 3})

	written, err := os.ReadFile(manager.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, line := range strings.Split(strings.TrimSuffix(string(written), "\n"), "\n") {
		key := sessionLineKey(line)
		want, ok := wantKeys[key]
		if !ok {
			continue
		}
		checked++
		got := topLevelKeyOrder(line)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s key order = %v, want the reference's %v", key, got, want)
		}
	}
	if checked < 7 {
		t.Fatalf("compared %d entry shapes, want at least the seven non-message types", checked)
	}
}

// sessionLineKey names the shape a line belongs to: its type, plus the message
// role for a message entry.
func sessionLineKey(line string) string {
	var head struct {
		Type    string `json:"type"`
		Message struct {
			Role string `json:"role"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &head); err != nil || head.Type == "" {
		return ""
	}
	if head.Type == "message" {
		return "message/" + head.Message.Role
	}
	return head.Type
}

// topLevelKeyOrder lists an object's keys in document order. Objects are
// traversed by tracking nesting depth, so a nested object's keys are skipped.
func topLevelKeyOrder(line string) []string {
	decoder := json.NewDecoder(strings.NewReader(line))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	var keys []string
	// depth counts open containers; a key name is only read at depth 0, and the
	// value that follows it is skipped by the same token walk.
	depth := 0
	expectKey := true
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
			if depth == 0 {
				expectKey = true
			}
			continue
		}
		if depth == 0 && expectKey {
			if name, ok := token.(string); ok {
				keys = append(keys, name)
				expectKey = false
			}
			continue
		}
		if depth == 0 {
			expectKey = true
		}
	}
	return keys
}
