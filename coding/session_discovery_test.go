package coding

import (
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Session manager tests keyed to upstream session-manager.ts semantics.

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
