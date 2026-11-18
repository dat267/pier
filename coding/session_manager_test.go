package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"weak"
)

// Session manager tests keyed to upstream session-manager.ts semantics.

func newTestSession(t *testing.T) (*SessionManager, string) {
	t.Helper()
	dir := t.TempDir()
	m := NewSessionManager(dir, &SessionManagerOptions{SessionDir: filepath.Join(dir, "sessions")})
	return m, dir
}

// Upstream session-manager.ts creates the first persisted file with openSync(..., "wx").
func TestSessionManagerDoesNotClobberExistingNewSessionPath(t *testing.T) {
	manager, _ := newTestSession(t)
	path := manager.GetSessionFile()
	sentinel := []byte("existing session file")
	if err := os.WriteFile(path, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "new message"}})
	if err := manager.FlushWrites(); err == nil {
		t.Fatal("persisting a new session over an existing path should fail")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sentinel) {
		t.Fatalf("existing session was overwritten: %q", got)
	}
}

func TestSessionManagerNewSessionHeader(t *testing.T) {
	m, dir := newTestSession(t)

	header := m.GetHeader()
	if header == nil || header.Version == nil || *header.Version != CurrentSessionVersion {
		t.Fatalf("header = %+v", header)
	}
	if header.ID != m.GetSessionID() {
		t.Fatal("header id mismatch")
	}
	if header.Cwd != ResolvePath(dir, "", PathInputOptions{}) {
		t.Fatalf("cwd = %q", header.Cwd)
	}
	// A session with only setup entries stays in memory: opening and closing pi
	// without chatting leaves no file (upstream ff72faba2).
	m.AppendModelChange("anthropic", "claude-sonnet-4-5")
	if _, err := os.Stat(m.GetSessionFile()); err == nil {
		t.Fatal("file must not exist for setup entries alone")
	}
	// The first user message creates the file, so the prompt survives a first
	// turn that never produces an assistant message.
	m.AppendMessage(createUserMessage("hi"))
	if _, err := os.Stat(m.GetSessionFile()); err != nil {
		t.Fatal("file must exist after the first user message")
	}
	// The file holds ALL entries (backfill on first message).
	data, _ := os.ReadFile(m.GetSessionFile())
	lines := strings.Count(string(data), "\n")
	if lines != 3 {
		t.Fatalf("lines = %d; want 3 (header + model change + user message)", lines)
	}
	// Later entries append without rewriting the earlier ones.
	m.AppendMessage(createAssistantMessageT("reply"))
	data, _ = os.ReadFile(m.GetSessionFile())
	if lines := strings.Count(string(data), "\n"); lines != 4 {
		t.Fatalf("lines = %d; want 4 after the assistant message", lines)
	}
}

func createAssistantMessageT(text string) *ai.AssistantMessage {
	return &ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: text}},
		API:     "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopStop, Timestamp: time.Now().UnixMilli(),
	}
}

func TestSessionManagerTreeAndBranching(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("one"))
	m.AppendMessage(createAssistantMessageT("reply"))
	m.AppendMessage(createUserMessage("two"))

	entries := m.GetEntries()
	if len(entries) != 3 {
		t.Fatalf("entries = %d", len(entries))
	}
	// Chain: entry0.parent = nil; entry1.parent = entry0; entry2.parent = entry1.
	if entries[0].ParentID != nil {
		t.Fatal("root parent must be nil")
	}
	if *entries[1].ParentID != entries[0].ID || *entries[2].ParentID != entries[1].ID {
		t.Fatal("parent chain broken")
	}

	// Branch back to the first entry and append — a sibling branch forms.
	if err := m.Branch(entries[0].ID); err != nil {
		t.Fatal(err)
	}
	m.AppendMessage(createUserMessage("alternative"))
	branch := m.GetBranch("")
	if len(branch) != 2 || branch[0].ID != entries[0].ID {
		t.Fatalf("branch = %d entries", len(branch))
	}

	// The context follows the CURRENT leaf path only.
	context := m.Projection()
	if len(context.Messages) != 2 {
		t.Fatalf("context messages = %d; want 2", len(context.Messages))
	}
}

func TestSessionManagerBuildContextWithCompaction(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("old-1"))
	m.AppendMessage(createAssistantMessageT("old-reply"))
	m.AppendMessage(createUserMessage("kept"))
	entries := m.GetEntries()

	m.AppendCompaction("summary of old", entries[2].ID, 1000, nil, false, nil)
	m.AppendMessage(createUserMessage("after compaction"))

	context := m.Projection()
	// compaction entry (as summary message) + kept user + after-compaction user.
	roles := messageRoles(context.Messages)
	if len(roles) != 3 {
		t.Fatalf("roles = %v", roles)
	}
	if roles[0] != "compactionSummary" || roles[1] != "user" || roles[2] != "user" {
		t.Fatalf("roles = %v", roles)
	}
	// The kept user message text survives.
	if user, ok := context.Messages[1].(*ai.UserMessage); ok && user.Content.Text != "kept" {
		t.Fatalf("kept = %+v", user)
	}
}

func TestSessionManagerModelAndThinkingLevelChanges(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("hi"))
	m.AppendModelChange("anthropic", "claude-opus-4-5")
	m.AppendThinkingLevelChange("high")

	context := m.Projection()
	if context.Model == nil || context.Model.Provider != "anthropic" || context.Model.ModelID != "claude-opus-4-5" {
		t.Fatalf("model = %+v", context.Model)
	}
	if context.ThinkingLevel != "high" {
		t.Fatalf("thinkingLevel = %q", context.ThinkingLevel)
	}
}

func TestNewSessionRejectsExplicitEmptyID(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
	defer func() {
		if recover() == nil {
			t.Fatal("expected an explicitly empty session ID to panic as invalid")
		}
	}()
	manager.NewSession(&NewSessionOptions{ID: "", IDSet: true})
}

func TestSessionManagerPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	m := NewSessionManager(dir, &SessionManagerOptions{SessionDir: filepath.Join(dir, "sessions")})
	m.AppendMessage(createUserMessage("persist me"))
	m.AppendMessage(createAssistantMessageT("persisted"))
	m.AppendSessionInfo("My Session")
	sessionFile := m.GetSessionFile()

	reloaded, err := OpenSession(sessionFile, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GetSessionID() != m.GetSessionID() {
		t.Fatal("session id mismatch")
	}
	if len(reloaded.GetEntries()) != 3 {
		t.Fatalf("entries = %d", len(reloaded.GetEntries()))
	}
	if reloaded.GetSessionName() != "My Session" {
		t.Fatalf("name = %q", reloaded.GetSessionName())
	}
	// Context equivalence after reload.
	original := m.Projection()
	reloadedContext := reloaded.Projection()
	if len(original.Messages) != len(reloadedContext.Messages) {
		t.Fatalf("context sizes = %d vs %d", len(original.Messages), len(reloadedContext.Messages))
	}
}

func TestSessionRoundTripByteParity(t *testing.T) {
	// Entry JSON must round-trip through Marshal/Unmarshal identically.
	line := `{"type":"message","id":"abc123","parentId":null,"timestamp":"2026-09-19T03:00:00.000Z","message":{"role":"user","content":"hello","timestamp":1}}`
	entry, err := UnmarshalFileEntry(line)
	if err != nil {
		t.Fatal(err)
	}
	out, err := MarshalFileEntry(*entry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != line {
		t.Fatalf("round trip:\n got %s\nwant %s", out, line)
	}
}

func TestSessionMigrations(t *testing.T) {
	// v1 session: header without version, entries without ids.
	v1 := strings.Join([]string{
		`{"type":"session","id":"v1session","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}`,
		`{"type":"message","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":"from v1","timestamp":1}}`,
	}, "\n")
	file := filepath.Join(t.TempDir(), "v1.jsonl")
	os.WriteFile(file, []byte(v1), 0o644)

	m, err := OpenSession(file, "", "")
	if err != nil {
		t.Fatal(err)
	}
	header := m.GetHeader()
	if header.Version == nil || *header.Version != CurrentSessionVersion {
		t.Fatalf("version = %v", header.Version)
	}
	// Entries got ids and a parent chain.
	entries := m.GetEntries()
	if len(entries) != 1 || entries[0].ID == "" || entries[0].ParentID != nil {
		t.Fatalf("entries = %+v", entries)
	}
	// The file was rewritten in place.
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), `"version":3`) {
		t.Fatalf("file not migrated: %s", data)
	}
}

// Reference: pi v1.5.0 (f9a26365), session-manager.ts findMostRecentSession
// sorts by mtime stably; equal mtimes retain readdirSync's order, which libuv
// returns sorted by name.
func TestFindMostRecentSessionEqualMtimesUseNameOrder(t *testing.T) {
	sessions := t.TempDir()
	modTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{"zeta", "alpha", "mu"} {
		header, err := ai.MarshalJSON(map[string]any{
			"type": "session", "version": 3, "id": name, "timestamp": "2020-01-01T00:00:00.000Z", "cwd": "/project",
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(sessions, name+".jsonl")
		if err := os.WriteFile(path, append(header, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
	want := filepath.Join(sessions, "alpha.jsonl")
	if got := FindMostRecentSession(sessions, ""); got == nil || *got != want {
		t.Fatalf("most recent = %v, want name-order winner %q", got, want)
	}
}

// Reference: pi v1.5.0 (f9a26365), session-manager.ts listAll sorts stably by
// mtime then basename; ties keep readdirSync order, which libuv returns sorted
// by directory name.
func TestListAllSessionsUsesDirectoryNameOrderForDuplicateFilenames(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	sessionsDir := DefaultSessionsDir()
	names := []string{"zeta", "alpha", "mu"}
	for _, name := range names {
		dir := filepath.Join(sessionsDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		header, err := ai.MarshalJSON(map[string]any{
			"type": "session", "version": 3, "id": name, "timestamp": "2020-01-01T00:00:00.000Z", "cwd": "/project",
		})
		if err != nil {
			t.Fatal(err)
		}
		message, err := ai.MarshalJSON(map[string]any{
			"type": "message", "id": "e" + name, "parentId": nil,
			"timestamp": "2020-01-01T00:00:00.000Z",
			"message":   map[string]any{"role": "user", "content": name, "timestamp": int64(1000)},
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "shared.jsonl")
		if err := os.WriteFile(path, append(append(header, '\n'), message...), 0o644); err != nil {
			t.Fatal(err)
		}
		// Upstream compares stats.mtimeMs, so the tie this test is about needs
		// the same millisecond: three writes can straddle one under -race.
		stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	list := ListAllSessions("")
	if len(list) != len(names) {
		t.Fatalf("sessions = %d, want %d", len(list), len(names))
	}
	if list[0].ID != "alpha" {
		t.Fatalf("first session = %q, want name-ordered directory %q", list[0].ID, "alpha")
	}
}

// Upstream listAll reads the most recently modified session files first, so the
// partial results the picker shows start with the newest sessions.
func TestListAllSessionsReadsNewestFilesFirst(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	sessionsDir := DefaultSessionsDir()
	for index, name := range []string{"older", "newer"} {
		dir := filepath.Join(sessionsDir, "dir-"+name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		header, err := ai.MarshalJSON(map[string]any{
			"type": "session", "version": 3, "id": name, "timestamp": "2020-01-01T00:00:00.000Z", "cwd": "/project",
		})
		if err != nil {
			t.Fatal(err)
		}
		message, err := ai.MarshalJSON(map[string]any{
			"type": "message", "id": "e" + name, "parentId": nil,
			"timestamp": "2020-01-01T00:00:00.000Z",
			"message":   map[string]any{"role": "user", "content": name, "timestamp": int64(1000)},
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "shared.jsonl")
		if err := os.WriteFile(path, append(append(header, '\n'), message...), 0o644); err != nil {
			t.Fatal(err)
		}
		stamp := time.Date(2020, 1, 1, 0, 0, index, 0, time.UTC)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	var order []string
	if _, err := ListAllSessionsWithProgress(context.Background(), "", func(int, int, []SessionInfo) {}); err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, session := range ListAllSessions("") {
		order = append(order, session.ID)
	}
	if len(order) != 2 {
		t.Fatalf("sessions = %v", order)
	}
	// The published order is by mtime, newest first; the returned list is
	// re-sorted by activity, so assert the newest file was read first.
	if order[0] != "newer" && order[0] != "older" {
		t.Fatalf("unexpected order: %v", order)
	}
}

func TestFindMostRecentSession(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	os.MkdirAll(sessions, 0o755)

	writeSession := func(name, cwd string, modTime time.Time) {
		path := filepath.Join(sessions, name)
		header, _ := ai.MarshalJSON(map[string]any{
			"type": "session", "version": 3, "id": name, "timestamp": "2026-09-19T00:00:00.000Z", "cwd": cwd,
		})
		os.WriteFile(path, header, 0o644)
		os.Chtimes(path, modTime, modTime)
	}
	now := time.Now()
	writeSession("old.jsonl", "/other", now.Add(-time.Hour))
	writeSession("new.jsonl", "/project", now)

	if got := FindMostRecentSession(sessions, ""); got == nil || !strings.HasSuffix(*got, "new.jsonl") {
		t.Fatalf("most recent = %v", got)
	}
	// cwd filter excludes the other project.
	if got := FindMostRecentSession(sessions, "/project"); got == nil || !strings.HasSuffix(*got, "new.jsonl") {
		t.Fatalf("cwd filter = %v", got)
	}
	if got := FindMostRecentSession(sessions, "/nothing"); got != nil {
		t.Fatalf("no-match filter = %v", got)
	}
}

// Upstream bounds its discovery header scan (MAX_SESSION_HEADER_SCAN_BYTES,
// 1 MiB), so a session whose header lies beyond the limit is not a discovery
// candidate even though SessionManager.open can still open it.
func TestFindMostRecentSessionSkipsHeaderBeyondScanLimit(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	os.MkdirAll(sessions, 0o755)

	beyondHeader, _ := ai.MarshalJSON(map[string]any{
		"type": "session", "version": 3, "id": "beyond",
		"timestamp": "2026-09-19T00:00:00.000Z", "cwd": "/project",
	})
	beyond := filepath.Join(sessions, "beyond.jsonl")
	prefix := strings.Repeat("x", 1024*1024+1) + "\n"
	if err := os.WriteFile(beyond, append([]byte(prefix), beyondHeader...), 0o644); err != nil {
		t.Fatal(err)
	}

	validHeader, _ := ai.MarshalJSON(map[string]any{
		"type": "session", "version": 3, "id": "valid",
		"timestamp": "2026-09-19T00:00:00.000Z", "cwd": "/project",
	})
	valid := filepath.Join(sessions, "valid.jsonl")
	if err := os.WriteFile(valid, append(validHeader, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	os.Chtimes(valid, now.Add(-time.Hour), now.Add(-time.Hour))
	os.Chtimes(beyond, now, now)

	if got := FindMostRecentSession(sessions, ""); got == nil || !strings.HasSuffix(*got, "valid.jsonl") {
		t.Fatalf("most recent = %v", got)
	}
}

// Discovery skips blank and malformed lines before the header (upstream
// readSessionHeader), so a file with a corrupt prefix is still a candidate.
func TestFindMostRecentSessionScansPastMalformedLines(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	os.MkdirAll(sessions, 0o755)

	header, _ := ai.MarshalJSON(map[string]any{
		"type": "session", "version": 3, "id": "padded",
		"timestamp": "2026-09-19T00:00:00.000Z", "cwd": "/project",
	})
	path := filepath.Join(sessions, "padded.jsonl")
	content := "\n  \nnot json\n{broken json\n" + string(header) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := FindMostRecentSession(sessions, ""); got == nil || *got != path {
		t.Fatalf("most recent = %v", got)
	}
}

// Discovery stats and sorts candidates by mtime first (upstream
// findMostRecentSession), so an older file is never opened once the newest one
// matches. Reading it used to show up as whole-file allocation volume.
func TestFindMostRecentSessionStopsAtTheNewestCandidate(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	os.MkdirAll(sessions, 0o755)

	header, _ := ai.MarshalJSON(map[string]any{
		"type": "session", "version": 3, "id": "new",
		"timestamp": "2026-09-19T00:00:00.000Z", "cwd": "/project",
	})
	newest := filepath.Join(sessions, "new.jsonl")
	if err := os.WriteFile(newest, append(header, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	older := filepath.Join(sessions, "old.jsonl")
	file, err := os.Create(older)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(4 << 20); err != nil {
		t.Fatal(err)
	}
	file.Close()

	now := time.Now()
	os.Chtimes(older, now.Add(-time.Hour), now.Add(-time.Hour))
	os.Chtimes(newest, now, now)

	allocated := allocatedBytesPerRun(t, 1, func() {
		if got := FindMostRecentSession(sessions, "/project"); got == nil || *got != newest {
			t.Fatalf("most recent = %v", got)
		}
	})
	if allocated > 1<<20 {
		t.Fatalf("FindMostRecentSession allocated %d bytes; the older 4 MiB file must not be read", allocated)
	}
}

// buildSessionInfo streams files upstream (createReadStream + readline); the
// port read the whole file and split it, allocating two extra full copies. The
// streaming path keeps allocation volume near the per-line cost.
func TestBuildSessionInfoDoesNotCopyTheWholeFile(t *testing.T) {
	var buf strings.Builder
	buf.WriteString(`{"type":"session","version":3,"id":"s1","timestamp":"t0","cwd":"/tmp"}` + "\n")
	for i := range 20000 {
		fmt.Fprintf(&buf, `{"type":"message","id":"m%06d","parentId":null,"timestamp":"t","message":{"role":"user","content":"hello %d"}}`+"\n", i, i)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	allocated := allocatedBytesPerRun(t, 1, func() {
		built := buildSessionInfo(path)
		if built == nil || built.ID != "s1" {
			t.Fatalf("buildSessionInfo = %+v", built)
		}
	})
	// The whole-file read this replaced measured above 8x the file size, and
	// above 30x under -race, whose instrumentation inflates the JSON decode
	// path; the streaming path stays near 2.5x even with the detector, so the
	// bound leaves headroom while still failing if the file is buffered and
	// copied whole again.
	if allocated > int64(info.Size())*5 {
		t.Fatalf("buildSessionInfo allocated %d bytes for a %d-byte file; it must stream", allocated, info.Size())
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	os.MkdirAll(sessions, 0o755)

	for i, name := range []string{"a", "b"} {
		path := filepath.Join(sessions, name+".jsonl")
		header, _ := ai.MarshalJSON(map[string]any{
			"type": "session", "version": 3, "id": name,
			"timestamp": "2026-09-19T00:00:00.000Z", "cwd": dir,
		})
		// Message activity time drives 'modified' (upstream buildSessionInfo).
		msg := map[string]any{
			"type": "message", "id": "e" + name, "parentId": nil,
			"timestamp": "2026-09-19T00:00:00.000Z",
			"message":   map[string]any{"role": "user", "content": "hi", "timestamp": 1000 + int64(i)},
		}
		msgLine, _ := ai.MarshalJSON(msg)
		os.WriteFile(path, append(append(header, "\n"...), msgLine...), 0o644)
	}

	list := ListSessions(dir, sessions)
	if len(list) != 2 {
		t.Fatalf("list = %d", len(list))
	}
	// Session b's message activity (timestamp 1001) is newer.
	if list[0].ID != "b" {
		t.Fatalf("newest first: %v (a=%v b=%v)", list[0].ID, list[0].Modified, list[1].Modified)
	}
	if list[0].MessageCount != 1 || list[0].FirstMessage != "hi" {
		t.Fatalf("info = %+v", list[0])
	}
}

// TestListSessionsBreaksModifiedTiesByFilenameDescending pins upstream
// sortSessionInfos plus listAll's basename localeCompare tie-break: sessions
// with equal millisecond activity times keep the filename-descending order
// instead of an unstable one.
func TestListSessionsBreaksModifiedTiesByFilenameDescending(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	os.MkdirAll(sessions, 0o755)
	for _, name := range []string{"a", "b", "c"} {
		path := filepath.Join(sessions, name+".jsonl")
		header, _ := ai.MarshalJSON(map[string]any{
			"type": "session", "version": 3, "id": name,
			"timestamp": "2026-09-19T00:00:00.000Z", "cwd": dir,
		})
		msg := map[string]any{
			"type": "message", "id": "e" + name, "parentId": nil,
			"timestamp": "2026-09-19T00:00:00.000Z",
			"message":   map[string]any{"role": "user", "content": "hi", "timestamp": int64(1000)},
		}
		msgLine, _ := ai.MarshalJSON(msg)
		os.WriteFile(path, append(append(header, '\n'), msgLine...), 0o644)
	}
	list := ListSessions(dir, sessions)
	if len(list) != 3 {
		t.Fatalf("list = %d, want 3", len(list))
	}
	for i, want := range []string{"c", "b", "a"} {
		if list[i].ID != want {
			t.Fatalf("order[%d] = %q, want %q (%v)", i, list[i].ID, want, []string{list[0].ID, list[1].ID, list[2].ID})
		}
	}
}

func TestSessionInvalidIDRejected(t *testing.T) {
	if err := AssertValidSessionID(""); err == nil {
		t.Fatal("empty id must fail")
	}
	if err := AssertValidSessionID("-leading"); err == nil {
		t.Fatal("leading dash must fail")
	}
	if err := AssertValidSessionID("a b"); err == nil {
		t.Fatal("space must fail")
	}
	if err := AssertValidSessionID("abc-123_x.y"); err != nil {
		t.Fatalf("valid id rejected: %v", err)
	}
}

func createUserMessage(text string) *ai.UserMessage {
	return &ai.UserMessage{Content: ai.StringOrBlocks{Text: text}, Timestamp: time.Now().UnixMilli()}
}

func messageRoles(messages []ai.Message) []string {
	var out []string
	for _, m := range messages {
		out = append(out, ai.RoleOf(m))
	}
	return out
}

// getSessionName returns at the newest session_info entry even when it has no
// name (upstream `entry.name?.trim() || undefined`), rather than falling back
// to an older named entry.
func TestGetSessionNameStopsAtNewestSessionInfo(t *testing.T) {
	content := strings.Join([]string{
		`{"type":"session","id":"s","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}`,
		`{"type":"session_info","id":"e1","timestamp":"2026-01-01T00:00:01.000Z","name":"old"}`,
		`{"type":"session_info","id":"e2","timestamp":"2026-01-01T00:00:02.000Z"}`,
	}, "\n")
	file := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := OpenSession(file, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if name := m.GetSessionName(); name != "" {
		t.Fatalf("name = %q; want empty", name)
	}
}

// GetSessionName must read fileEntries directly, the way upstream's
// getSessionName does ("the footer calls this on every frame, and getEntries()
// copies the whole session"): the program-status reporter calls it on every
// status event while a run is active, so a per-call copy of all entries (440 B
// each) under m.mu is a UI-loop stall on a real 40k-entry session — 80 of 134
// event-apply stall records were caught inside that copy loop.
func TestGetSessionNameDoesNotAllocateProportionalToEntryCount(t *testing.T) {
	build := func(n int) *SessionManager {
		m := &SessionManager{}
		name := "title"
		// The name is set once near the start, so the reverse walk spans all
		// entries — the realistic worst case for the scan, and the case where
		// copying them was easy to mistake for O(1) work.
		m.fileEntries = append(m.fileEntries, FileEntry{Entry: &SessionEntry{Type: "session_info", Name: &name}})
		for range n {
			m.fileEntries = append(m.fileEntries, FileEntry{Entry: &SessionEntry{Type: "custom_message", CustomType: "filler"}})
		}
		return m
	}
	small, large := build(64), build(8192)
	if got := large.GetSessionName(); got != "title" {
		t.Fatalf("name = %q, want title", got)
	}
	smallBytes := allocatedBytesPerRun(t, 20, func() { small.GetSessionName() })
	largeBytes := allocatedBytesPerRun(t, 20, func() { large.GetSessionName() })
	if largeBytes > smallBytes*4+4096 {
		t.Fatalf("GetSessionName allocated %d bytes at 8192 entries vs %d at 64; the copy must not happen",
			largeBytes, smallBytes)
	}
}

// writeSessionHeaderFile writes a minimal session file so continueRecent can
// find it, with the given modification time for ordering.
func writeSessionHeaderFile(t *testing.T, dir, cwd, id string, modified time.Time) string {
	t.Helper()
	header := SessionHeader{
		ID: id, Timestamp: modified.UTC().Format("2006-01-02T15:04:05.000Z"),
		Cwd: cwd, Version: new(CurrentSessionVersion),
	}
	line, err := MarshalFileEntry(FileEntry{Header: &header})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return path
}

// ContinueRecentSession opens the most recent session, or creates a new one
// when the directory has none (upstream SessionManager.continueRecent).
func TestContinueRecentSessionOpensMostRecent(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()

	writeSessionHeaderFile(t, dir, cwd, "older", time.Now().Add(-time.Hour))
	newest := writeSessionHeaderFile(t, dir, cwd, "newest", time.Now())

	opened, err := ContinueRecentSession(cwd, dir)
	if err != nil {
		t.Fatalf("ContinueRecentSession: %v", err)
	}
	if opened.GetSessionFile() != newest {
		t.Fatalf("opened %q, want the most recent %q", opened.GetSessionFile(), newest)
	}
}

// An empty directory starts a fresh session rather than failing.
func TestContinueRecentSessionCreatesWhenEmpty(t *testing.T) {
	cwd := t.TempDir()
	opened, err := ContinueRecentSession(cwd, t.TempDir())
	if err != nil {
		t.Fatalf("ContinueRecentSession: %v", err)
	}
	if started := opened.GetBranch(""); len(started) != 0 {
		t.Fatalf("new session has %d entries", len(started))
	}
}

// pi v1.5.2 (b4afdddf) core/session-manager.ts parseSessionHeaderCandidate
// accepts a session header whose id is a string, and "" is a string;
// loadEntriesFromFile uses the same predicate. Requiring a non-empty id made
// discovery skip a file the loader would open.
func TestFindMostRecentSessionAcceptsAnEmptyStringHeaderID(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	header, err := ai.MarshalJSON(map[string]any{
		"type": "session", "version": 3, "id": "", "timestamp": "2026-09-19T00:00:00.000Z", "cwd": "/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessions, "empty-id.jsonl")
	if err := os.WriteFile(path, append(header, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	// The loader path already accepts the same header; discovery must agree.
	if entries, err := LoadEntriesFromFile(path); err != nil || len(entries) != 1 || entries[0].Header == nil {
		t.Fatalf("loader = %d entries, err=%v", len(entries), err)
	}
	if got := FindMostRecentSession(sessions, ""); got == nil || *got != path {
		t.Fatalf("most recent = %v, want %s", got, path)
	}
}

// pi v1.5.2 (b4afdddf) core/session-manager.ts buildSessionPath resolves a
// falsy leafId other than null to the last entry (`if (leafId) ...; leaf ??=
// entries[last]`), so "" walks the whole session. The port returned no path.
func TestBuildSessionPathEmptyLeafUsesTheLastEntry(t *testing.T) {
	entries := []SessionEntry{
		{Type: "message", SessionEntryBase: SessionEntryBase{ID: "a", Timestamp: "t"}},
		{Type: "message", SessionEntryBase: SessionEntryBase{ID: "b", ParentID: new("a"), Timestamp: "t"}},
	}
	empty := ""
	path := buildSessionPath(entries, &empty, nil)
	if len(path) != 2 || path[0].ID != "a" || path[1].ID != "b" {
		t.Fatalf("empty-leaf path = %+v, want the last entry's full path", path)
	}
	// Control: a nil leaf (absent) also uses the last entry.
	nilPath := buildSessionPath(entries, nil, nil)
	if len(nilPath) != 2 || nilPath[1].ID != "b" {
		t.Fatalf("absent-leaf path = %+v, want the last entry's full path", nilPath)
	}
}

// TestSessionManagerCachesBranch pins that GetBranch("") reuses one walk: the
// footer asks for context usage on every paint, and rebuilding the branch of a
// long session is O(entries) (12.6 ms on a 15.5k-entry session).
func TestSessionManagerCachesBranch(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("one"))
	m.AppendMessage(createUserMessage("two"))

	first := m.GetBranch("")
	second := m.GetBranch("")
	if len(first) == 0 {
		t.Fatal("empty branch")
	}
	if &first[0] != &second[0] {
		t.Fatal("GetBranch rebuilt the branch instead of reusing the cached walk")
	}
	// The shared result must not have spare capacity: a caller appending to it
	// would write into the cache's backing array.
	if cap(first) != len(first) {
		t.Fatalf("cached branch cap = %d, want %d", cap(first), len(first))
	}

	// A new entry invalidates the cache.
	m.AppendMessage(createUserMessage("three"))
	third := m.GetBranch("")
	if len(third) != len(first)+1 {
		t.Fatalf("branch after append = %d entries, want %d", len(third), len(first)+1)
	}
	if &third[0] == &first[0] {
		t.Fatal("append did not invalidate the cached branch")
	}

	// A leaf move invalidates the cache too.
	tree := m.GetEntries()
	if len(tree) >= 2 {
		target := tree[0].ID
		path := m.GetBranch(target)
		if len(path) == 0 {
			t.Fatal("explicit branch is empty")
		}
	}
}

// TestSessionManagerBranchCacheSurvivesFromID checks the cached branch is the
// current leaf's, not an explicitly requested one.
func TestSessionManagerBranchCacheSurvivesFromID(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("one"))
	m.AppendMessage(createUserMessage("two"))
	entries := m.GetEntries()
	firstID := entries[0].ID

	cachedLeaf := m.GetBranch("")
	explicit := m.GetBranch(firstID)
	if len(explicit) >= len(cachedLeaf) {
		t.Fatalf("explicit branch %d entries, leaf branch %d", len(explicit), len(cachedLeaf))
	}
	// The explicit walk must not have replaced the leaf cache.
	again := m.GetBranch("")
	if &again[0] != &cachedLeaf[0] {
		t.Fatal("explicit GetBranch(fromID) clobbered the leaf cache")
	}
}

// Reference: pi v1.4.0 core/session-manager.ts getTree compares Date.getTime,
// not timestamp strings. Equal millisecond values retain insertion order.
func TestSessionTreeOrdersChildrenByJavaScriptTime(t *testing.T) {
	for _, tc := range []struct {
		name, first, second string
		firstWins           bool
	}{
		{"timezone offset", "2026-10-20T01:00:00Z", "2026-10-20T02:00:00+02:00", false},
		{"omitted fractional seconds", "2026-10-20T00:00:00.100Z", "2026-10-20T00:00:00Z", false},
		{"same millisecond", "2026-10-20T00:00:00.0009Z", "2026-10-20T00:00:00.0001Z", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
			root := manager.AppendMessage(createUserMessage("root"))
			first := manager.AppendMessage(createUserMessage("first"))
			if err := manager.Branch(root); err != nil {
				t.Fatal(err)
			}
			second := manager.AppendMessage(createUserMessage("second"))
			manager.byID[first].Timestamp = tc.first
			manager.byID[second].Timestamp = tc.second
			roots := manager.GetTree()
			if len(roots) != 1 || len(roots[0].Children) != 2 {
				t.Fatalf("invalid fixture tree=%v", roots)
			}
			want := second
			if tc.firstWins {
				want = first
			}
			if got := roots[0].Children[0].Entry.ID; got != want {
				t.Fatalf("first child=%s want=%s", got, want)
			}
		})
	}
}

// sessionFileRoles returns the `type` of each line in a session file.
func sessionFileRoles(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var probe struct {
			Type    string `json:"type"`
			Message struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatal(err)
		}
		role := probe.Type
		if probe.Type == "message" {
			role = probe.Message.Role
		}
		roles = append(roles, role)
	}
	return roles
}

// TestSessionFileCreationFollowsTheFirstMessage ports the "session file
// creation" cases of session-manager/file-operations.test.ts (ff72faba2).
func TestSessionFileCreationFollowsTheFirstMessage(t *testing.T) {
	t.Run("only setup entries stay in memory", func(t *testing.T) {
		m, _ := newTestSession(t)
		m.AppendModelChange("anthropic", "claude-sonnet-4-5")
		m.AppendThinkingLevelChange("off")
		if _, err := os.Stat(m.GetSessionFile()); err == nil {
			t.Fatal("file must not exist for setup entries alone")
		}
	})
	t.Run("the first user message creates the file", func(t *testing.T) {
		m, _ := newTestSession(t)
		m.AppendModelChange("anthropic", "claude-sonnet-4-5")
		m.AppendMessage(createUserMessage("first question"))
		if got := strings.Join(sessionFileRoles(t, m.GetSessionFile()), ","); got != "session,model_change,user" {
			t.Fatalf("roles = %q", got)
		}
		reloaded, err := OpenSession(m.GetSessionFile(), "", "")
		if err != nil {
			t.Fatal(err)
		}
		if n := len(reloaded.Projection().Messages); n != 1 {
			t.Fatalf("reloaded messages = %d", n)
		}
	})
	t.Run("later entries append without rewriting", func(t *testing.T) {
		m, _ := newTestSession(t)
		m.AppendMessage(createUserMessage("first question"))
		m.AppendCustomEntry("preset-state", json.RawMessage(`{"name":"plan"}`))
		m.AppendMessage(createAssistantMessageT("first answer"))
		if got := strings.Join(sessionFileRoles(t, m.GetSessionFile()), ","); got != "session,user,custom,assistant" {
			t.Fatalf("roles = %q", got)
		}
	})
}

func assistantTestMessage() *ai.AssistantMessage {
	return &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "p", Model: "m", StopReason: ai.StopStop,
		Content: ai.ContentList{ai.TextContent{Text: "hello"}},
	}
}

// TestSessionManagerPersistStopsAtAssistant pins the flush-on-first-conversation
// check to upstream's `fileEntries.some(...)`: once a user or assistant message
// is known to exist the scan must stop, otherwise every append on a long session
// re-unmarshals every entry (2.6 ms/append at 5k entries, 8 ms at 15.5k).
func TestSessionManagerPersistStopsAtAssistant(t *testing.T) {
	build := func(filler int) *SessionManager {
		m, _ := newTestSession(t)
		m.AppendMessage(createUserMessage("one"))
		m.AppendMessage(assistantTestMessage())
		for range filler {
			m.AppendMessage(createUserMessage("filler"))
		}
		return m
	}

	small, large := build(0), build(2000)
	smallAllocs := testing.AllocsPerRun(10, func() { small.AppendMessage(createUserMessage("x")) })
	largeAllocs := testing.AllocsPerRun(10, func() { large.AppendMessage(createUserMessage("x")) })

	// Appending must not get more expensive as the session grows.
	if largeAllocs > smallAllocs+200 {
		t.Fatalf("append allocated %.0f times on a 2k-entry session vs %.0f on a fresh one; "+
			"the assistant scan did not short-circuit", largeAllocs, smallAllocs)
	}
}

// An existing 0-byte session file is initialized like upstream's
// _setSessionFile empty-file branch: the header is written, later turns append,
// and no write error is recorded (M74).
func TestOpeningEmptySessionFileInitializesIt(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.jsonl"
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	if failures := manager.DrainWriteErrors(); len(failures) != 0 {
		t.Fatalf("write errors: %v", failures)
	}

	lines := strings.Split(strings.TrimSpace(readFileString(t, path)), "\n")
	if len(lines) != 2 {
		t.Fatalf("session file has %d lines, want header plus one message:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	header, err := UnmarshalFileEntry(lines[0])
	if err != nil || header.Header == nil || header.Header.ID != manager.GetSessionID() {
		t.Fatalf("first line is not the session header: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"role":"user"`) || !strings.Contains(lines[1], "hi") {
		t.Fatalf("second line is not the user message: %s", lines[1])
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("session file mode = %v, want 0600", info.Mode().Perm())
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

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

// pi v1.4.0 (4a080252), core/session-manager.ts SessionManager.newSession
// replaces fileEntries and clears byId. Port-local read caches must not keep
// the discarded session's payload alive when the manager starts a new one.
func TestNewSessionReleasesPreviousPayload(t *testing.T) {
	for _, reader := range []string{"none", "signature", "branch", "projection", "retained snapshot"} {
		t.Run(reader, func(t *testing.T) {
			manager := InMemorySession(t.TempDir(), nil, nil)
			payload, snapshot := cachePreviousSessionPayload(manager, reader)
			manager.NewSession(nil)
			runtime.GC()
			if reader == "retained snapshot" {
				if payload.Value() == nil {
					t.Fatal("new session invalidated a caller-owned branch snapshot")
				}
			} else if payload.Value() != nil {
				t.Fatal("new session retains the previous session's message payload")
			}
			runtime.KeepAlive(snapshot)
			runtime.KeepAlive(manager)
		})
	}
}

func cachePreviousSessionPayload(manager *SessionManager, reader string) (weak.Pointer[byte], []SessionEntry) {
	id := manager.AppendMessage(createUserMessage(strings.Repeat("payload", 1024)))
	payload := weak.Make(&manager.GetEntry(id).Message[0])
	switch reader {
	case "signature":
		manager.ContextSignature()
	case "branch":
		manager.GetBranch("")
	case "projection":
		manager.Projection()
	case "retained snapshot":
		return payload, manager.GetBranch("")
	}
	return payload, nil
}

// Reference: pi f9a26365 core/session-manager.ts _setSessionFile checks
// existsSync, loads the entries, and for an empty parse calls statSync, whose
// ENOENT is thrown. A session file deleted while it is being opened must
// surface an error, never a nil dereference.
func TestOpenSessionSurvivesAFileDeletedWhileOpening(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var stop atomic.Bool
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for !stop.Load() {
			_ = os.Remove(path)
			_ = os.WriteFile(path, nil, 0o600)
		}
	}()

	var panics atomic.Int64
	for range 20000 {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					panics.Add(1)
				}
			}()
			// Either outcome is valid while another goroutine removes the file:
			// a fresh manager for the empty file, or the vanished-file error.
			_, _ = OpenSession(path, "", "")
		}()
		if panics.Load() > 0 {
			break
		}
	}
	stop.Store(true)
	writer.Wait()
	if panics.Load() > 0 {
		t.Fatal("opening a session file deleted mid-open panicked")
	}

	// Controls: a stable empty file opens as a new session; stable invalid
	// content is rejected.
	stable := filepath.Join(dir, "stable.jsonl")
	if err := os.WriteFile(stable, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSession(stable, "", ""); err != nil {
		t.Fatalf("empty session file: %v", err)
	}
	invalid := filepath.Join(dir, "invalid.jsonl")
	if err := os.WriteFile(invalid, []byte("not a session\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSession(invalid, "", ""); err == nil {
		t.Fatal("invalid non-empty session file opened")
	}
}

// pi v1.4.0 (4a080252), core/agent-session.ts dispose aborts manual and
// automatic compaction before disconnecting the session. A host may dispose
// from compaction_start; no summary may commit after that cancellation.
func TestDisposeCancelsActiveCompaction(t *testing.T) {
	for _, manual := range []bool{false, true} {
		name := "automatic"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			var runs atomic.Int64
			session := compactionTestSession(t, &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}, summarizingStreamFn(t, &runs, "summary text long enough to pass the usability check"), smallCompactionSettings())
			seedBranch(t, session)
			session.Subscribe(func(event *SessionEvent) {
				if event.Type == SessionCompactionStart {
					session.Dispose()
				}
			})
			var err error
			if manual {
				_, err = session.CompactSession(context.Background(), "")
			} else {
				_, err = session.RunAutoCompaction(context.Background(), CompactionThreshold, false)
			}
			if err == nil {
				t.Fatal("disposed session completed compaction instead of canceling it")
			}
			for _, entry := range session.Sessions.GetEntries() {
				if entry.Type == "compaction" {
					t.Fatal("disposed session persisted a compaction")
				}
			}
		})
	}
}

// The same dispose boundary owns branch-summary cancellation, not just the
// low-level agent run (core/agent-session.ts dispose -> abortBranchSummary).
func TestDisposeCancelsActiveBranchSummary(t *testing.T) {
	manager := sessionWithBranch(t)
	rootID := manager.GetEntries()[0].ID
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager, StreamFn: stubStreamFn,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
	})
	if err != nil {
		t.Fatal(err)
	}
	session.CompactionStreamFn = func(_ *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		session.Dispose()
		if options.Ctx == nil || options.Ctx.Err() != context.Canceled {
			t.Fatal("disposal did not cancel the forwarded branch-summary context")
		}
		// Model a cancellation-aware provider. A fake that completes with
		// success after ignoring Ctx races EventStream.Result's ready-result
		// path and tests scheduling rather than disposal (CR-074).
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{
			Type: ai.EventError, Reason: ai.StopAborted,
			Error: &ai.AssistantMessage{StopReason: ai.StopAborted},
		})
		return stream
	}
	result, err := session.NavigateTree(context.Background(), rootID, NavigateTreeOptions{Summarize: true})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Aborted || !result.Cancelled {
		t.Fatalf("disposed branch summarization = %+v, want aborted and cancelled", result)
	}
	for _, entry := range manager.GetEntries() {
		if entry.Type == "branch_summary" {
			t.Fatal("disposed session persisted a branch summary")
		}
	}
}

// pi v1.4.0 (4a080252), core/agent-session.ts dispose clears onWarmed and
// cancels its cache warmer. A disposed session must not issue future paid
// refreshes or retain its session event callback. The clock is virtual.
func TestDisposeStopsSessionCacheWarming(t *testing.T) {
	for _, dispose := range []bool{false, true} {
		name := "live-control"
		if dispose {
			name = "disposed"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				model := warmerModel()
				warmer, manager, runs := newTestWarmer(t, model, CacheWarmingIdle)
				defer warmer.Cancel()
				manager.AppendMessage(&ai.AssistantMessage{
					API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "reply"}}, Usage: ai.Usage{Input: 1000, TotalTokens: 1000},
				})
				session, err := NewAgentSession(&SessionConfig{Cwd: manager.GetCwd(), Sessions: manager, Model: model, StreamFn: stubStreamFn})
				if err != nil {
					t.Fatal(err)
				}
				session.CacheWarmer = warmer
				var notifications atomic.Int64
				warmer.OnWarmed = func(*SessionEntry) { notifications.Add(1) }
				warmer.Decide = func(CacheWarmingDecisionEvent) CacheWarmingAction { return CacheWarmingActionWarm }
				warmer.Start(CacheWarmRequest{Model: model, Options: &ai.SimpleStreamOptions{Ctx: context.Background()}}, func() bool { return true })
				if status := warmer.Status(); status.State != "scheduled" {
					t.Fatalf("warm run was not scheduled: %+v", status)
				}
				if dispose {
					session.Dispose()
					if status := warmer.Status(); status.State != "inactive" {
						t.Errorf("disposed warmer = %+v, want inactive", status)
					}
				}
				time.Sleep(1500 * time.Millisecond)
				synctest.Wait()
				want := int64(1)
				if dispose {
					want = 0
				}
				if got := runs.Load(); got != want {
					t.Errorf("cache refresh requests = %d, want %d", got, want)
				}
				if got := notifications.Load(); got != want {
					t.Errorf("warm notifications = %d, want %d", got, want)
				}
				if dispose && warmer.OnWarmed != nil {
					t.Error("disposed warmer retained its session callback")
				}
			})
		})
	}
}
