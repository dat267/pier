//go:build linux

package coding

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 packages/coding-agent/src/core/session-manager.ts and
// session-export.ts use Node defaults; Pier's
// owner-only modes are the D289 security divergence.
func TestSessionManagerPersistsFilesWithPrivateMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)
	root := t.TempDir()
	manager := NewSessionManager(root, &SessionManagerOptions{SessionDir: filepath.Join(root, "sessions")})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "persist"}})
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(manager.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("manager session file mode = %#o; want private %#o", got, 0o600)
	}
}

func TestOpeningExistingSessionMakesFilePrivate(t *testing.T) {
	manager, _ := newTestSession(t)
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "persist"}})
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	path, dir := manager.GetSessionFile(), manager.GetSessionDir()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSession(path, dir, ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("opened session file mode = %#o; want private %#o", got, 0o600)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("opened session directory mode = %#o; want private %#o", got, 0o700)
	}
}

// session-manager.ts receives UTF-8-decoded directory entry names from readdir.
func TestListSessionsSkipsInvalidUTF8Filename(t *testing.T) {
	dir := t.TempDir()
	manager := NewSessionManager(dir, &SessionManagerOptions{SessionDir: filepath.Join(dir, "sessions")})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "persist"}})
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	invalidPath := filepath.Join(manager.SessionDir(), string([]byte{0xff})+".jsonl")
	if err := os.Rename(manager.GetSessionFile(), invalidPath); err != nil {
		t.Fatal(err)
	}
	if got := ListSessions(manager.GetCwd(), manager.SessionDir()); len(got) != 0 {
		t.Fatalf("invalid UTF-8 session names = %+v; want no readable entries", got)
	}
}

// session-manager.ts listAll receives UTF-8-decoded names from readdir.
func TestListAllSessionsSkipsInvalidUTF8DirectoryNames(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	sessionsDir := filepath.Join(agentDir, "sessions")
	invalidDir := filepath.Join(sessionsDir, string([]byte{0xff})+"project")
	if err := os.MkdirAll(invalidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	header := SessionHeader{ID: "session", Timestamp: "2026-01-01T00:00:00.000Z", Cwd: "/project"}
	line, err := MarshalFileEntry(FileEntry{Header: &header})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidDir, "session.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ListAllSessions(""); len(got) != 0 {
		t.Fatalf("sessions in invalid UTF-8 path = %+v; want none", got)
	}
}

func TestSessionFileUsesPrivateMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	file := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(file, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionEntries(file, nil); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("session file mode = %#o; want private %#o", got, 0o600)
	}
}

func TestForkedSessionUsesPrivateModes(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	root := t.TempDir()
	forkDir := filepath.Join(root, "forked")
	manager, err := writeForkedSession(filepath.Join(root, "project"), forkDir, true, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(manager.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("forked session mode = %#o; want private %#o", got, 0o600)
	}
	dirInfo, err := os.Stat(forkDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("forked session directory mode = %#o; want private %#o", got, 0o700)
	}
}

func TestSessionManagerUsesPrivateDirectoryMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	dir := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	_ = NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: dir})
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("session manager directory mode = %#o; want private %#o", got, 0o700)
	}
}

func TestSessionExportUsesPrivateDirectoryMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
	dir := filepath.Join(t.TempDir(), "exports")
	if _, err := ExportSessionToJsonl(manager, filepath.Join(dir, "export.jsonl"), nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("export directory mode = %#o; want private %#o", got, 0o700)
	}
}

func TestSessionExportUsesPrivateFileMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
	path := filepath.Join(t.TempDir(), "export.jsonl")
	if err := os.WriteFile(path, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportSessionToJsonl(manager, path, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("export file mode = %#o; want private %#o", got, 0o600)
	}
}

func TestSessionDirectoryUsesPrivateMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	root := t.TempDir()
	cwd, agentDir := filepath.Join(root, "project"), filepath.Join(root, "agent")
	dir := DefaultSessionDir(cwd, agentDir)
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	dir = DefaultSessionDir(cwd, agentDir)
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("session directory mode = %#o; want private %#o", got, 0o700)
	}
}

// A session opened by explicit path only inherits its file's directory for
// placement; sessions derived from it (new, clone, fork, import) must not chmod
// a directory the user chose for the file (M73). D289 hardens pier's own session
// directories, not theirs.
func TestPathOpenedSessionReportsDerivedDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := OpenSession(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if opened.SessionDirManaged() {
		t.Fatal("a directory inherited from the opened file reported as managed")
	}
	if got := opened.GetSessionDir(); got != dir {
		t.Fatalf("derived session dir = %q, want %q", got, dir)
	}
	if got := sessionDirForReplacement(opened); got != "" {
		t.Fatalf("replacement dir = %q, want the file's directory to speak for itself", got)
	}
}

func TestManagedSessionReportsItsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	managed := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: dir})
	if !managed.SessionDirManaged() {
		t.Fatal("an explicit session store reported as derived")
	}
	if got := sessionDirForReplacement(managed); got != dir {
		t.Fatalf("replacement dir = %q, want %q", got, dir)
	}
}

func TestDerivedSessionDirKeepsParentDirectoryMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := OpenSession(path, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// The /new shape: keep the directory for placement, skip hardening.
	fresh := NewSessionManager(opened.GetCwd(), &SessionManagerOptions{
		SessionDir:        opened.GetSessionDir(),
		SessionDirDerived: !opened.SessionDirManaged(),
	})
	if got := fresh.GetSessionDir(); got != dir {
		t.Fatalf("derived session dir = %q, want %q", got, dir)
	}
	if fresh.GetSessionFile() != "" && filepath.Dir(fresh.GetSessionFile()) != dir {
		t.Fatalf("derived session file = %q, want under %q", fresh.GetSessionFile(), dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("derived session directory mode = %#o; want the user's %#o", got, 0o755)
	}
}

func TestDerivedForkKeepsParentDirectoryMode(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}` + "\n" +
		`{"type":"message","id":"m1","parentId":null,"timestamp":"t","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := OpenSession(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	leaf := opened.GetLeafID()
	if leaf == nil {
		t.Fatal("fixture has no leaf")
	}
	if _, err := ForkSessionAtEntryDerived(opened, *leaf, opened.GetCwd(), opened.GetSessionDir(), nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("fork directory mode = %#o; want the user's %#o", got, 0o755)
	}
}
