package coding

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// TestCustomEntryDataField pins the field name: the custom entry payload
// serializes as `data` (upstream CustomEntry.data), not `details`.
func TestCustomEntryDataField(t *testing.T) {
	manager := NewSessionManager("/tmp/proj", &SessionManagerOptions{Persist: new(false)})
	payload := json.RawMessage(`{"hello":"world"}`)
	manager.AppendCustomEntry("note", payload)

	entries := manager.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	entry := entries[0]
	if string(entry.Data) != string(payload) {
		t.Fatalf("data = %s", entry.Data)
	}
	if len(entry.Details) != 0 {
		t.Fatalf("details should be empty: %s", entry.Details)
	}

	// The serialized line uses `data` and round-trips.
	line, err := MarshalFileEntry(FileEntry{Entry: &entry})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(line, `"data":{"hello":"world"}`) || strings.Contains(line, `"details"`) {
		t.Fatalf("line = %s", line)
	}
	parsed, err := UnmarshalFileEntry(line)
	if err != nil || parsed.Entry == nil {
		t.Fatalf("parse: %v", err)
	}
	if string(parsed.Entry.Data) != string(payload) {
		t.Fatalf("parsed data = %s", parsed.Entry.Data)
	}
}

// entryIDPattern is upstream's short-id shape (generateId slices a random UUID
// to eight hex characters).
var entryIDPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)

// TestEntryIDsAreShortAndRandom pins that shape. generateID sliced a UUIDv7,
// whose leading characters are the millisecond timestamp, so every id generated
// in the same ~4.3 s window matched, the retry loop burned 100 UUIDs, and the
// fallback handed out full-length ids: port sessions are full of 32-hex entry
// ids where upstream has 8.
func TestEntryIDsAreShortAndRandom(t *testing.T) {
	m := newTestSessionForProjection(t)
	for range 8 {
		m.AppendMessage(createUserMessage("a message that gets an entry id"))
	}

	seen := map[string]bool{}
	for _, entry := range m.GetEntries() {
		if !entryIDPattern.MatchString(entry.ID) {
			t.Fatalf("entry %q has id %q; want eight hex characters", entry.Type, entry.ID)
		}
		if seen[entry.ID] {
			t.Fatalf("duplicate entry id %q", entry.ID)
		}
		seen[entry.ID] = true
	}
}

// The session scanner's entry points must reject input that ends before a value
// starts: the depth/offset guard is what keeps scanJSONValueAt from indexing
// past the slice.
func TestSessionScanRejectsAnEmptyValue(t *testing.T) {
	for _, input := range [][]byte{nil, {}, []byte(""), []byte(" ")} {
		if _, ok := skipJSONValue(input); ok {
			t.Fatalf("skipJSONValue(%q) accepted an empty value", input)
		}
	}
	if _, ok := skipJSONValue([]byte(`"unterminated`)[:1]); ok {
		t.Fatal("a lone quote was accepted")
	}
	if _, ok := skipJSONValue([]byte("\"\x01\"")); ok {
		t.Fatal("a raw control character was accepted inside a string")
	}
}

// A session with no entries has no path, and buildSessionPath must say so
// without indexing the empty slice (the leaf fallback only applies when there
// is an entry to fall back to).
func TestBuildSessionPathOnAnEmptySession(t *testing.T) {
	if path := buildSessionPath(nil, nil, nil); path != nil {
		t.Fatalf("path = %v, want none", path)
	}
	missing := "not-there"
	if path := buildSessionPath(nil, &missing, nil); path != nil {
		t.Fatalf("path for a missing leaf = %v, want none", path)
	}
}

// Every truncated prefix of a session line must be rejected, not panic: the
// scanner reads bytes at the cursor, so a line that ends mid-structure has to
// stop at the bounds check (a hand-edited or half-written file is reachable).
func TestScanSessionLineToleratesTruncatedLines(t *testing.T) {
	line := []byte(`{"type":"message","id":"a","parentId":null,"timestamp":"t","message":{"role":"user","content":[]}}`)
	if _, ok := scanSessionLine(line); !ok {
		t.Fatal("the complete line was rejected")
	}
	for n := 0; n < len(line); n++ {
		prefix := string(line[:n])
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("prefix %q panicked: %v", prefix, recovered)
				}
			}()
			if _, ok := scanSessionLine(line[:n]); ok {
				t.Fatalf("prefix %q was accepted", prefix)
			}
		}()
	}
}

// A branch summary without a summary projects nothing (session-manager.ts
// sessionEntryToContextMessages returns [] for a falsy summary), so a crafted
// entry cannot inject an empty message into the resumed transcript.
func TestSessionEntryToContextMessagesSkipsEmptyBranchSummary(t *testing.T) {
	entry := &SessionEntry{Type: "branch_summary", Summary: "", FromID: "root"}
	if messages := SessionEntryToContextMessages(entry); len(messages) != 0 {
		t.Fatalf("messages = %+v, want none for an empty summary", messages)
	}
	withText := &SessionEntry{Type: "branch_summary", Summary: "done", FromID: "root"}
	if messages := SessionEntryToContextMessages(withText); len(messages) != 1 {
		t.Fatalf("messages = %+v, want one summary message", messages)
	}
}
