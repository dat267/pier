package interactive

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/dat267/pier/coding"
)

// TestForkIntoSessionForMissingCwd ports upstream InteractiveMode /
// switchToForkedSession (interactive-mode.ts, #11060): a session whose stored cwd
// is gone is copied into the current cwd with a new id and the app switches to
// the copy, leaving the original untouched.
func TestForkIntoSessionForMissingCwd(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	app, cleanup := newTestAppPersisted(t)
	defer cleanup()

	missingCwd := t.TempDir()
	orphan := makePersistedSessionIn(t, missingCwd, t.TempDir(), "from the orphan")
	if err := os.Remove(missingCwd); err != nil {
		t.Fatalf("remove cwd: %v", err)
	}
	original, err := os.ReadFile(orphan.GetSessionFile())
	if err != nil {
		t.Fatalf("read orphan: %v", err)
	}

	cwd := app.sessionMgr.GetCwd()
	var forked *ForkedSessionResult
	var forkErr error
	app.forkIntoSessionAsync(context.Background(), orphan.GetSessionFile(), cwd, func(result *ForkedSessionResult, err error) {
		forked, forkErr = result, err
	})
	if forkErr != nil {
		t.Fatalf("fork: %v", forkErr)
	}
	if forked == nil || forked.Cancelled {
		t.Fatalf("forked = %+v", forked)
	}
	if forked.SessionID == orphan.GetSessionID() {
		t.Fatal("fork kept the original session id")
	}
	if forked.Cwd != cwd {
		t.Fatalf("fork cwd = %q, want %q", forked.Cwd, cwd)
	}
	// The app switched to the fork.
	if app.sessionMgr.GetSessionID() != forked.SessionID {
		t.Fatalf("app session = %s, want the fork %s", app.sessionMgr.GetSessionID(), forked.SessionID)
	}
	// The fork's header points at the original and carries the new cwd; its
	// entries travel whole.
	entries, err := coding.LoadEntriesFromFile(app.sessionMgr.GetSessionFile())
	if err != nil {
		t.Fatalf("load fork: %v", err)
	}
	sourceEntries, err := coding.LoadEntriesFromFile(orphan.GetSessionFile())
	if err != nil {
		t.Fatalf("load orphan: %v", err)
	}
	header := entries[0].Header
	if header == nil || header.ID != forked.SessionID || header.Cwd != cwd {
		t.Fatalf("header = %+v", header)
	}
	if header.ParentSession == nil || *header.ParentSession != coding.ResolvePath(orphan.GetSessionFile(), "", coding.PathInputOptions{}) {
		t.Fatalf("parentSession = %v", header.ParentSession)
	}
	if len(entries) != 1+len(sourceEntries) {
		t.Fatalf("fork entries = %d, want the source's %d entries after the header", len(entries), len(sourceEntries))
	}
	// The original session file is unchanged.
	after, err := os.ReadFile(orphan.GetSessionFile())
	if err != nil {
		t.Fatalf("read orphan after fork: %v", err)
	}
	if string(after) != string(original) {
		t.Fatal("fork rewrote the original session file")
	}
}

// TestForkIntoSessionDropsForkWhenSwitchVetoed covers upstream
// switchToForkedSession's cleanup: a vetoed switch removes the fork so
// cancelling leaves no copy behind.
func TestForkIntoSessionDropsForkWhenSwitchVetoed(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	app, cleanup := newTestAppPersisted(t)
	defer cleanup()

	missingCwd := t.TempDir()
	orphan := makePersistedSessionIn(t, missingCwd, t.TempDir(), "from the orphan")
	sessionDir := app.sessionMgr.GetSessionDir()
	before, err := os.ReadDir(sessionDir)
	if err != nil {
		t.Fatalf("read session dir: %v", err)
	}
	sessionID := app.sessionMgr.GetSessionID()

	var forkPath string
	app.sessions.SwitchSession = func(_ context.Context, sessionPath string, _ string, onDone func(*SessionSwitchResult, error)) {
		forkPath = sessionPath
		onDone(&SessionSwitchResult{Cancelled: true}, nil)
	}
	var forked *ForkedSessionResult
	var forkErr error
	app.forkIntoSessionAsync(context.Background(), orphan.GetSessionFile(), app.sessionMgr.GetCwd(), func(result *ForkedSessionResult, err error) {
		forked, forkErr = result, err
	})
	if forkErr != nil {
		t.Fatalf("fork: %v", forkErr)
	}
	if forked == nil || !forked.Cancelled {
		t.Fatalf("forked = %+v", forked)
	}
	if forkPath == "" {
		t.Fatal("the fork was never switched to")
	}
	if _, err := os.Stat(forkPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fork file %q still exists: %v", forkPath, err)
	}
	after, err := os.ReadDir(sessionDir)
	if err != nil {
		t.Fatalf("read session dir: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("session dir has %d files, want %d", len(after), len(before))
	}
	if app.sessionMgr.GetSessionID() != sessionID {
		t.Fatal("a vetoed switch changed the session")
	}
}
