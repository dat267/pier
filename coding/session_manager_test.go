package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
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

// Reference: pi v1.4.0 (4a080252), session-manager.ts findMostRecentSession
// sorts by mtime stably; equal mtimes retain readdirSync's native order.
func TestFindMostRecentSessionEqualMtimesUseNativeDirectoryOrder(t *testing.T) {
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
	directory, err := os.Open(sessions)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	want := filepath.Join(sessions, entries[0].Name())
	if got := FindMostRecentSession(sessions, ""); got == nil || *got != want {
		t.Fatalf("most recent = %v, want stable native-order winner %q", got, want)
	}
}

// Reference: pi v1.4.0 (4a080252), session-manager.ts listAll preserves
// native directory order when candidate mtime and basename tie.
func TestListAllSessionsPreservesNativeOrderForDuplicateFilenames(t *testing.T) {
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
	}
	directory, err := os.Open(sessionsDir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if len(entries) == 0 {
		t.Fatal("session directory fixture is empty")
	}
	list := ListAllSessions("")
	if len(list) != len(names) {
		t.Fatalf("sessions = %d, want %d", len(list), len(names))
	}
	if list[0].ID != entries[0].Name() {
		t.Fatalf("first session = %q, want native-order directory %q", list[0].ID, entries[0].Name())
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
