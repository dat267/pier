package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// parseSessionLines decodes JSONL lines through the same entry decoder the
// session loader uses.
func parseSessionLines(t *testing.T, lines ...string) []FileEntry {
	t.Helper()
	out := make([]FileEntry, 0, len(lines))
	for _, line := range lines {
		entry, err := UnmarshalFileEntry(line)
		if err != nil {
			t.Fatalf("parse %q: %v", line, err)
		}
		out = append(out, *entry)
	}
	return out
}

// Reference: pi v1.4.0 coding-agent/test/session-manager/migration.test.ts
// "should add id/parentId to v1 entries" and session-manager.ts migrateV1ToV2.
// A v1 entry gains an 8-character id and a parentId chain, and the header ends
// at the current version.
func TestSessionMigrationV1AddsTreeIDs(t *testing.T) {
	entries := parseSessionLines(t,
		`{"type":"session","id":"sess-1","timestamp":"2025-01-01T00:00:00Z","cwd":"/tmp"}`,
		`{"type":"message","timestamp":"2025-01-01T00:00:01Z","message":{"role":"user","content":"hi","timestamp":1}}`,
		`{"type":"message","timestamp":"2025-01-01T00:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"api":"test","provider":"test","model":"test","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0},"stopReason":"stop","timestamp":2}}`,
	)
	if !migrateToCurrentVersion(entries) {
		t.Fatal("v1 entries must migrate")
	}
	if version := entries[0].Header.Version; version == nil || *version != CurrentSessionVersion {
		t.Fatalf("header version = %v, want %d", version, CurrentSessionVersion)
	}
	first, second := entries[1].Entry, entries[2].Entry
	for _, entry := range []*SessionEntry{first, second} {
		if len(entry.ID) != 8 || !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(entry.ID) {
			t.Fatalf("id = %q, want 8 hex characters", entry.ID)
		}
	}
	if first.ParentID != nil {
		t.Fatalf("first parentId = %v, want null", *first.ParentID)
	}
	if second.ParentID == nil || *second.ParentID != first.ID {
		t.Fatalf("second parentId = %v, want %q", second.ParentID, first.ID)
	}

	// The header had no version, so the assignment appends it last.
	header, err := MarshalFileEntry(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"session","id":"sess-1","timestamp":"2025-01-01T00:00:00Z","cwd":"/tmp","version":3}` + "\n"; header != want {
		t.Fatalf("rewritten v1 header = %s", header)
	}

	// Upstream assigns id/parentId on the parsed object, so they follow the
	// members the v1 file already had instead of being inserted after "type".
	rewritten := mustMarshalSessionEntry(t, entries[1].Entry)
	want := regexp.MustCompile(`^\{"type":"message","timestamp":"2025-01-01T00:00:01Z","message":\{"role":"user","content":"hi","timestamp":1\},"id":"[0-9a-f]{8}","parentId":null\}$`)
	if !want.MatchString(rewritten) {
		t.Fatalf("rewritten v1 entry = %s", rewritten)
	}
}

// Reference: migration.test.ts "should be idempotent (skip already migrated)".
func TestSessionMigrationIsIdempotent(t *testing.T) {
	entries := parseSessionLines(t,
		`{"type":"session","version":2,"id":"sess-1","timestamp":"2025-01-01T00:00:00Z","cwd":"/tmp"}`,
		`{"type":"message","id":"abc12345","parentId":null,"timestamp":"2025-01-01T00:00:01Z","message":{"role":"user","content":"hi","timestamp":1}}`,
		`{"type":"message","id":"def67890","parentId":"abc12345","timestamp":"2025-01-01T00:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"api":"test","provider":"test","model":"test","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0},"stopReason":"stop","timestamp":2}}`,
	)
	migrateToCurrentVersion(entries)
	if entries[1].Entry.ID != "abc12345" || entries[2].Entry.ID != "def67890" {
		t.Fatalf("ids changed: %q, %q", entries[1].Entry.ID, entries[2].Entry.ID)
	}
	if parent := entries[2].Entry.ParentID; parent == nil || *parent != "abc12345" {
		t.Fatalf("parentId = %v, want abc12345", parent)
	}

	before := mustMarshalSessionEntry(t, entries[2].Entry)
	if migrateToCurrentVersion(entries) {
		t.Fatal("a current-version file must not migrate again")
	}
	if after := mustMarshalSessionEntry(t, entries[2].Entry); after != before {
		t.Fatalf("re-migration changed the entry:\nbefore %s\nafter  %s", before, after)
	}
}

// Reference: load-entries.test.ts "migrates entries restored with an older
// header" and "adopts headerless entries as current-version without migrating
// them", via migrateV2ToV3.
func TestSessionMigrationHookMessageRole(t *testing.T) {
	restored := InMemorySession("/project", nil, parseSessionLines(t,
		`{"type":"session","version":2,"id":"v2-session","timestamp":"2026-01-01T00:00:00Z","cwd":"/project"}`,
		`{"type":"message","id":"abc12345","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"hookMessage","content":"from a hook","timestamp":1}}`,
	))
	entry := restored.GetEntries()[0]
	if version := restored.GetHeader().Version; version == nil || *version != CurrentSessionVersion {
		t.Fatalf("header version = %v, want %d", version, CurrentSessionVersion)
	}
	if !strings.Contains(string(entry.Message), `"role":"custom"`) || entry.ID != "abc12345" {
		t.Fatalf("migrated entry = %s", mustMarshalSessionEntry(t, &entry))
	}

	headerless := InMemorySession("/project", nil, parseSessionLines(t,
		`{"type":"message","id":"abc12345","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"hookMessage","content":"from a hook","timestamp":1}}`,
	))
	if got := string(headerless.GetEntries()[0].Message); !strings.Contains(got, `"role":"hookMessage"`) {
		t.Fatalf("headerless entry was migrated: %s", got)
	}
}

// Reference: session-manager.ts _loadEntries/_rewriteFile. A migrated file is
// re-stringified from the parsed objects, so untouched members keep their
// order, unknown members survive, and only the migration's own changes appear.
func TestSessionMigrationRewritePreservesMemberOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	original := `{"type":"session","version":2,"id":"v2-session","timestamp":"2026-01-01T00:00:00Z","cwd":"/project"}
{"type":"message","id":"abc12345","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"hookMessage","content":"from a hook","timestamp":1},"customField":{"b":1,"a":2}}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSession(path, dir, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"session","version":3,"id":"v2-session","timestamp":"2026-01-01T00:00:00Z","cwd":"/project"}
{"type":"message","id":"abc12345","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"custom","content":"from a hook","timestamp":1},"customField":{"b":1,"a":2}}
`
	if string(data) != want {
		t.Fatalf("rewritten file:\n got %s\nwant %s", data, want)
	}
}

func mustMarshalSessionEntry(t *testing.T, entry *SessionEntry) string {
	t.Helper()
	line, err := MarshalFileEntry(FileEntry{Entry: entry})
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(line, "\n")
}

// Reference: session-manager.ts createBranchedSession drops label entries and
// re-chains the retained path. Because a loaded entry is re-stringified from
// its parsed object, the rewritten parentId replaces the stored one; emitting
// the retained line unchanged would keep pointing at the dropped label.
func TestBranchedSessionRechainsRetainedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tree.jsonl")
	original := `{"type":"session","version":3,"id":"sess","timestamp":"2026-01-01T00:00:00Z","cwd":"/project"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"hi","timestamp":1}}
{"type":"label","id":"lbl","parentId":"m1","timestamp":"2026-01-01T00:00:02Z","targetId":"m1","label":"bookmark"}
{"type":"message","id":"m2","parentId":"lbl","timestamp":"2026-01-01T00:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"api":"test","provider":"test","model":"test","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0},"stopReason":"stop","timestamp":2}}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	branched, err := manager.CreateBranchedSession("m2")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(branched)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("branched file = %s", data)
	}
	var label SessionEntry
	if err := json.Unmarshal([]byte(lines[3]), &label); err != nil {
		t.Fatal(err)
	}
	if label.Type != "label" || label.TargetID != "m1" || label.Label == nil || *label.Label != "bookmark" || label.ParentID == nil || *label.ParentID != "m2" {
		t.Fatalf("recreated label = %+v", label)
	}
	if want := `{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-01-01T00:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"api":"test","provider":"test","model":"test","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0},"stopReason":"stop","timestamp":2}}`; lines[2] != want {
		t.Fatalf("branched entry:\n got %s\nwant %s", lines[2], want)
	}
}

// Reference: session-export.ts pushes `{ ...entry, parentId }`, so members the
// port does not model survive an export of a loaded branch.
func TestSessionExportPreservesLoadedMembers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "loaded.jsonl")
	original := `{"type":"session","version":3,"id":"sess","timestamp":"2026-01-01T00:00:00Z","cwd":"/project"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"hi","timestamp":1},"extensionField":{"z":1,"a":2}}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(SerializeSessionBranch(manager, nil), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("export = %q", lines)
	}
	if !strings.Contains(lines[1], `"extensionField":{"z":1,"a":2}`) {
		t.Fatalf("exported entry dropped a loaded member: %s", lines[1])
	}
	if !strings.Contains(lines[1], `"parentId":null`) {
		t.Fatalf("exported entry lost its parentId: %s", lines[1])
	}
}

// Reference: session-manager.ts migrateV1ToV2 converts a compaction entry's
// firstKeptEntryIndex to firstKeptEntryId, deletes the legacy member, and only
// resolves targets that already carry an id (a forward reference becomes an
// omitted member, like a JavaScript undefined value).
func TestSessionMigrationV1CompactionTarget(t *testing.T) {
	backward := parseSessionLines(t,
		`{"type":"session","id":"s","timestamp":"2025-01-01T00:00:00Z","cwd":"/tmp"}`,
		`{"type":"message","timestamp":"2025-01-01T00:00:01Z","message":{"role":"user","content":"hi","timestamp":1},"extra":true}`,
		`{"type":"compaction","timestamp":"2025-01-01T00:00:02Z","summary":"s","tokensBefore":5,"firstKeptEntryIndex":1}`,
	)
	migrateToCurrentVersion(backward)
	target := backward[1].Entry.ID
	rewritten := mustMarshalSessionEntry(t, backward[2].Entry)
	wantBackward := regexp.MustCompile(`^\{"type":"compaction","timestamp":"2025-01-01T00:00:02Z","summary":"s","tokensBefore":5,"id":"[0-9a-f]{8}","parentId":"` + target + `","firstKeptEntryId":"` + target + `"\}$`)
	if !wantBackward.MatchString(rewritten) {
		t.Fatalf("backward compaction = %s", rewritten)
	}
	// The unknown message member keeps its position ahead of the added tree ids.
	targetLine := mustMarshalSessionEntry(t, backward[1].Entry)
	if !strings.Contains(targetLine, `"extra":true,"id":"`+target+`","parentId":null}`) {
		t.Fatalf("migrated message = %s", targetLine)
	}

	forward := parseSessionLines(t,
		`{"type":"session","id":"s","timestamp":"2025-01-01T00:00:00Z","cwd":"/tmp"}`,
		`{"type":"compaction","timestamp":"2025-01-01T00:00:01Z","summary":"s","tokensBefore":5,"firstKeptEntryIndex":2}`,
		`{"type":"message","timestamp":"2025-01-01T00:00:02Z","message":{"role":"user","content":"hi","timestamp":1}}`,
	)
	migrateToCurrentVersion(forward)
	if got := mustMarshalSessionEntry(t, forward[1].Entry); strings.Contains(got, "firstKeptEntryId") || strings.Contains(got, "firstKeptEntryIndex") {
		t.Fatalf("forward compaction = %s", got)
	}
}
