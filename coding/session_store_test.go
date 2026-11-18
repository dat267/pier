package coding

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
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

// Reference: pi v1.4.0 core/session-manager.ts loadEntriesFromFile returns no
// entries unless the first parsed entry is a session header with a string id,
// so SessionManager._setSessionFile rejects a non-session file instead of
// loading its entries.
func TestOpenSessionRejectsAFileWithoutASessionHeader(t *testing.T) {
	message := `{"type":"message","id":"a","parentId":null,"timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"hi","timestamp":1}}` + "\n"
	for name, content := range map[string]string{
		"message only":        message,
		"junk before message": "garbage\n" + message,
		"non-session header":  `{"type":"compaction","id":"c","timestamp":"t"}` + "\n" + message,
	} {
		path := filepath.Join(t.TempDir(), "session.jsonl")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenSession(path, "", ""); err == nil || !strings.Contains(err.Error(), "not a valid pi session") {
			t.Errorf("%s: OpenSession error = %v; want a not-a-valid-pi-session error", name, err)
		}
	}
}

// An empty file is a fresh session, as upstream's zeroentry path rewrites it.
func TestOpenSessionInitializesAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSession(path, "", ""); err != nil {
		t.Fatalf("OpenSession(empty) error = %v", err)
	}
}

// A new session persists its leading system message only when it declares tools:
// a toolless prompt carries no tool loadout to restore (D237), and an empty
// entry would be written otherwise.
func TestNewSessionPersistsTheSystemEntryOnlyWithTools(t *testing.T) {
	tempAgentDir(t)
	build := func(tools []agent.AgentTool) int {
		manager := NewSessionManager(t.TempDir(), nil)
		session, err := NewAgentSession(&SessionConfig{
			Cwd: t.TempDir(), Sessions: manager, StreamFn: stubStreamFn, Tools: tools,
			Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Dispose()
		return len(manager.GetEntries())
	}
	if got := build(nil); got != 0 {
		t.Fatalf("entries without tools = %d, want none", got)
	}
	if got := build([]agent.AgentTool{testTool("read")}); got != 1 {
		t.Fatalf("entries with tools = %d, want the system entry", got)
	}
}

// Reference: pi v1.4.0 (4a080252), sessionEntryToContextMessages returns the
// particular entry's message, never another entry's data with a matching ID.
func TestEntryMessagesCachesOnlyCanonicalPayloads(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "original"}, Timestamp: 1})
	entry := manager.GetEntries()[0]
	cached := manager.Projection().Messages[0]
	if got := manager.EntryMessages(&entry); len(got) != 1 || got[0] != cached {
		t.Fatal("a canonical descriptor snapshot did not share the cached message")
	}

	foreign := entry
	// The replacement has the same byte length, so a length-only guard is not
	// enough to distinguish an external descriptor from the cached source.
	foreign.Message = json.RawMessage(strings.Replace(string(entry.Message), "original", "modified", 1))
	for _, id := range []string{entry.ID, "other-session", ""} {
		foreign.ID = id
		messages := manager.EntryMessages(&foreign)
		if len(messages) != 1 || messages[0].(*ai.UserMessage).Content.Text != "modified" {
			t.Fatalf("external entry %q reused the wrong payload: %#v", id, messages)
		}
	}
	if messages := manager.EntryMessages(&entry); len(messages) != 1 || messages[0] != cached || messages[0].(*ai.UserMessage).Content.Text != "original" {
		t.Fatal("an external descriptor polluted the canonical cache")
	}

	custom := entry
	custom.Type, custom.CustomType, custom.Content = "custom_message", "note", json.RawMessage(`"custom"`)
	messages := manager.EntryMessages(&custom)
	if len(messages) != 1 {
		t.Fatalf("synthetic messages = %#v", messages)
	}
	if synthetic, ok := messages[0].(*ai.CustomMessage); !ok || synthetic.Role != "custom" {
		t.Fatalf("synthetic entry reused a message with the same ID: %#v", messages)
	}
	manager.NewSession(nil)
	if messages := manager.EntryMessages(&entry); len(messages) != 1 || messages[0] == cached || messages[0].(*ai.UserMessage).Content.Text != "original" {
		t.Fatal("a stale descriptor after reset reused the old manager cache")
	}
}

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

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts getEntries()
// filters the header and returns a new array. Large resumed sessions call this
// during setup and transcript replay; growing a Go slice repeatedly copies the
// whole descriptor list several times before the first frame.
func TestGetEntriesAllocationBudget(t *testing.T) {
	const count = 512
	entries := make([]FileEntry, count+1)
	version := CurrentSessionVersion
	entries[0].Header = &SessionHeader{Version: &version, ID: "session", Cwd: t.TempDir()}
	for i := range count {
		entries[i+1].Entry = &SessionEntry{Type: "custom", ID: fmt.Sprint(i), CustomType: "test"}
	}
	manager := InMemorySession(entries[0].Header.Cwd, nil, entries)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	copied := manager.GetEntries()
	runtime.ReadMemStats(&after)
	if len(copied) != count {
		t.Fatalf("entries = %d, want %d", len(copied), count)
	}
	// Twice the final slice size allows allocator rounding but excludes
	// repeated allocation/copy of growing slices. No wall-clock threshold.
	budget := uint64(count) * uint64(reflect.TypeFor[SessionEntry]().Size()) * 2
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > budget {
		t.Fatalf("copying %d entries allocated %d bytes, want at most %d", count, allocated, budget)
	}
	copied[0].ID = "changed"
	if manager.GetEntries()[0].ID != "0" {
		t.Fatal("mutating a returned entry changed the manager's descriptor")
	}
	if got := InMemorySession(t.TempDir(), nil, nil).GetEntries(); got != nil {
		t.Fatalf("empty manager entries = %#v, want nil", got)
	}
}

// Reference: pi v1.5.2 b4afdddf core/session-manager.ts loadEntriesFromFile
// calls appendFileSync when the final JSONL entry has no newline; a failed
// repair must fail the open before a later append can concatenate entries.
func TestOpenSessionReportsFailedTrailingNewlineRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := "{\"type\":\"session\",\"version\":3,\"id\":\"s\",\"timestamp\":\"2026-01-01T00:00:00.000Z\",\"cwd\":\"/tmp\"}\n" +
		"{\"type\":\"message\",\"id\":\"m\",\"parentId\":null,\"timestamp\":\"2026-01-01T00:00:01.000Z\",\"message\":{\"role\":\"user\",\"content\":\"hello\",\"timestamp\":1}}"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("simulated newline repair failure")
	original := sessionLineWriter
	sessionLineWriter = func(gotPath, line string) error {
		if gotPath == path && line == "\n" {
			return wantErr
		}
		return original(gotPath, line)
	}
	t.Cleanup(func() { sessionLineWriter = original })

	if _, err := OpenSession(path, "", ""); !errors.Is(err, wantErr) {
		t.Fatalf("OpenSession error = %v, want the failed newline repair", err)
	}
}

func TestLoadEntriesFromFileReportsFailedTrailingNewlineRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := "{\"type\":\"session\",\"version\":3,\"id\":\"s\",\"timestamp\":\"2026-01-01T00:00:00.000Z\",\"cwd\":\"/tmp\"}\n" +
		"{\"type\":\"message\",\"id\":\"m\",\"parentId\":null,\"timestamp\":\"2026-01-01T00:00:01.000Z\",\"message\":{\"role\":\"user\",\"content\":\"hello\",\"timestamp\":1}}"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("simulated newline repair failure")
	original := sessionLineWriter
	sessionLineWriter = func(gotPath, line string) error {
		if gotPath == path && line == "\n" {
			return wantErr
		}
		return original(gotPath, line)
	}
	t.Cleanup(func() { sessionLineWriter = original })

	if _, err := LoadEntriesFromFile(path); !errors.Is(err, wantErr) {
		t.Fatalf("LoadEntriesFromFile error = %v, want the failed newline repair", err)
	}
}

// fastScanJSONString is the candidate: try the escape-free scanner first and
// fall back to the strict one. Both must accept exactly the same inputs.
func fastScanJSONString(s []byte, start int) (int, bool) {
	if _, n, ok := scanPlainStringBytes(s[start:]); ok {
		return start + n, true
	}
	return scanJSONStringStrict(s, start)
}

// TestSessionJSONStringScanMatchesStrictScan pins the fast dispatch to the
// strict scanner's accept set (pi v1.5.0 session-manager.ts parseSessionEntryLine
// only ever sees strings JSON.parse accepts).
func TestSessionJSONStringScanMatchesStrictScan(t *testing.T) {
	inputs := []string{
		`"plain"`, `""`, `"a\"b"`, `"a\\b"`, `"a\/b"`, `"a\u0041b"`, `"a\u00e9"`,
		`"a\uZZZZb"`, `"a\u00"`, `"a\u"`, `"a\qb"`, `"a` + "\x00" + `b"`,
		`"a` + "\x1f" + `b"`, `"a` + "\x7f" + `b"`, `"a` + "\xff" + `b"`,
		`"a` + "\xed\xa0\x80" + `b"`, `"é"`, `"a` + "\t" + `b"`, `"a` + "\n" + `b"`,
		`"unterminated`, `"`, ``, `"a"b`, `"a"` + " ", `"-"`, `"null"`, `"x\\"`,
		`"a` + "\u0080" + `b"`, `"a` + "\u2028" + `b"`, strings.Repeat("é", 40),
	}
	for _, in := range inputs {
		wantN, wantOK := scanJSONStringStrict([]byte(in), 0)
		gotN, gotOK := fastScanJSONString([]byte(in), 0)
		if gotOK != wantOK || (gotOK && gotN != wantN) {
			t.Errorf("%q: fast=(%d,%v) want=(%d,%v)", in, gotN, gotOK, wantN, wantOK)
		}
		if wantOK && !json.Valid([]byte(in[:wantN])) {
			t.Errorf("%q: strict scanner accepted invalid JSON", in)
		}
	}
}

// BenchmarkSessionJSONStringScan measures the dispatch over a realistic mix.
func BenchmarkSessionJSONStringScan(b *testing.B) {
	line := []byte(`{"role":"assistant","content":[{"type":"text","text":"Here is a fairly long answer with ASCII only, which is the common case in these session files."}],"timestamp":1762336467000}`)
	b.SetBytes(int64(len(line)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := scanJSONValueAt(line, 0, 0); !ok {
			b.Fatal("scan failed")
		}
	}
}

func BenchmarkSessionJSONStringScanStrict(b *testing.B) {
	s := []byte(`"Here is a fairly long answer with ASCII only, which is the common case in these session files."`)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := scanJSONStringStrict(s, 0); !ok {
			b.Fatal("scan failed")
		}
	}
}

func BenchmarkSessionJSONStringScanPlain(b *testing.B) {
	s := []byte(`"Here is a fairly long answer with ASCII only, which is the common case in these session files."`)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, ok := scanPlainStringBytes(s); !ok {
			b.Fatal("scan failed")
		}
	}
}

// Reference: pi v1.5.0 (f9a26365) reads a session line with JSON.parse, which
// accepts an unpaired surrogate escape and re-emits it from JSON.stringify.
// encoding/json/v2 rejects one at the lexer (A486), so a line the reference
// keeps was dropped here, and a v1/v2-to-v3 migration then rewrote the file
// without it. Relaxing the decode cannot change input that already decoded.
const (
	surrogateSessionKeep = `{"type":"custom","id":"keep","parentId":null,"timestamp":"t","customType":"x","data":{"a":1}}`
	surrogateSessionDrop = `{"type":"custom","id":"surr","parentId":"keep","timestamp":"t","customType":"x","data":"\ud83d"}`
)

func writeSurrogateSession(t *testing.T, dir, version string, lines ...string) string {
	t.Helper()
	header := `{"type":"session","version":` + version + `,"id":"h","timestamp":"2026-01-01T00:00:00.000Z","cwd":"` + dir + `"}`
	path := filepath.Join(dir, "s.jsonl")
	body := header + "\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A lone-surrogate entry survives a load and the migration's rewrite of the file.
func TestLoneSurrogateEntrySurvivesMigration(t *testing.T) {
	dir := t.TempDir()
	path := writeSurrogateSession(t, dir, "2", surrogateSessionKeep, surrogateSessionDrop)

	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if got := len(manager.GetBranch("")); got != 2 {
		t.Errorf("loaded %d entries, want 2: the lone-surrogate entry was dropped", got)
	}
	// The migration rewrites the file; the entry must still be there.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"id":"surr"`) {
		t.Errorf("the migration deleted the lone-surrogate entry from the session file:\n%s", after)
	}
}

// The message reaches the transcript, not just the entry list.
func TestLoneSurrogateMessageReachesProjection(t *testing.T) {
	dir := t.TempDir()
	good := `{"type":"message","id":"m1","parentId":null,"timestamp":"t","message":{"role":"user","content":[{"type":"text","text":"ok"}]}}`
	bad := `{"type":"message","id":"m2","parentId":"m1","timestamp":"t","message":{"role":"user","content":[{"type":"text","text":"\ud83d"}]}}`
	path := writeSurrogateSession(t, dir, "3", good, bad)

	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if got := len(manager.GetBranch("")); got != 2 {
		t.Fatalf("loaded %d entries, want 2", got)
	}
	if got := len(manager.Projection().Messages); got != 2 {
		t.Errorf("projected %d messages, want 2: the lone-surrogate message was dropped", got)
	}
}

// A duplicate member name is also valid for JSON.parse (last value wins), and
// json/v2 rejects it by default; the fast scanner keeps the line, so the strict
// decoder must not drop it either.
func TestDuplicateMemberNameEntrySurvivesLoad(t *testing.T) {
	dir := t.TempDir()
	dup := `{"type":"custom","id":"dup","parentId":null,"timestamp":"t","customType":"x","data":{"a":1,"a":2}}`
	path := writeSurrogateSession(t, dir, "3", dup)

	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if got := len(manager.GetBranch("")); got != 1 {
		t.Errorf("loaded %d entries, want 1: the duplicate-member entry was dropped", got)
	}
}

// persistEntry decides whether to create the session file by calling
// hasConversation, which re-decodes every entry's JSON on every append until a
// user or assistant message exists. That is O(n^2) in the number of entries
// before the first such message, and it runs under SessionManager.mu, so it
// also lengthens the run-lifecycle lock hold that an idle bash append takes
// (A483).
//
// Reference: pi v1.5.0 f9a26365 core/session-manager.ts:1167 _hasConversation
// reads `e.message.role` from the already-parsed entry, so it never decodes;
// the scan is linear in the array but has no per-entry parse cost.
//
// Invariant: the work of one append must not grow with the number of entries
// already appended.
func TestAppendCostDoesNotGrowBeforeFirstConversation(t *testing.T) {
	manager, _ := newTestSession(t)
	// A bash execution message is a "message" entry whose role is neither
	// user nor assistant, so it never ends the scan.
	msg := CreateBashExecutionMessage("echo", "x", nil, false, false, "", false, 0, nil)

	timeAppends := func(count int) time.Duration {
		start := time.Now()
		for i := 0; i < count; i++ {
			manager.AppendMessage(msg)
		}
		return time.Since(start)
	}
	// Warm: JITs, first allocation, and the initial file decision.
	timeAppends(50)
	early := timeAppends(200)
	timeAppends(600)
	late := timeAppends(200)

	// The same 200 appends must cost about the same early and late. A factor of
	// four is far looser than the measured quadratic growth and far tighter
	// than the noise on a warm filesystem.
	if late > 4*early {
		t.Fatalf("append cost grew with entry count: first 200 = %v, last 200 = %v (%.1fx)",
			early, late, float64(late)/float64(early))
	}
	t.Logf("first 200 = %v, last 200 = %v", early, late)
}
