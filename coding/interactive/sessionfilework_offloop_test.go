package interactive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/offloop"
	"github.com/dat267/pier/tui"
)

// wireOffLoopSessionWork installs the queue and post seam the command's
// composition wires, and returns a pump that runs the posted callback.
func wireOffLoopSessionWork(t *testing.T, app *App) func() {
	t.Helper()
	queue := offloop.New()
	t.Cleanup(queue.Stop)
	posted := make(chan func(), 1)
	app.sessionQueue = queue
	app.postToUI = func(fn func()) { posted <- fn }
	// The rename's seams are snapshotted by the wiring at construction.
	app.sessions.RenameQueue = queue
	app.sessions.PostToUI = app.postToOwnerLoop
	return func() {
		select {
		case fn := <-posted:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("the session-file work never reached the owner loop")
		}
	}
}

// Opening a session parses the whole file (57ms on a 25,583-entry session), so
// it runs off the owner loop and applies on it (D442).
func TestSwitchSessionOpensOffTheOwnerLoop(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	source := writeImportableSession(t, t.TempDir())
	pump := wireOffLoopSessionWork(t, app)

	before := app.sessionMgr.GetSessionID()
	applied := make(chan struct{}, 1)
	app.switchSessionAsync(context.Background(), source, "", func(*SessionSwitchResult, error) {
		applied <- struct{}{}
	})
	if app.sessionMgr.GetSessionID() != before {
		t.Fatal("the session was opened and applied on the owner loop")
	}
	pump()
	select {
	case <-applied:
	case <-time.After(5 * time.Second):
		t.Fatal("the switch never completed")
	}
	if app.sessionMgr.GetSessionID() == before {
		t.Fatal("the switch did not apply")
	}
}

// Writing a fork serializes the whole branch (measured 519ms on a 25,583-entry
// session), so it runs off the owner loop too.
func TestForkSessionWritesOffTheOwnerLoop(t *testing.T) {
	app, cleanup := newTestAppPersisted(t)
	defer cleanup()
	appendTurn(t, app, "hello")
	leaf := app.sessionMgr.GetLeafID()
	if leaf == nil {
		t.Fatal("no leaf after appending a turn")
	}
	pump := wireOffLoopSessionWork(t, app)

	before := app.sessionMgr.GetSessionID()
	applied := make(chan struct{}, 1)
	app.forkAtEntryAsync(context.Background(), *leaf, true, func(*SelectorForkResult, error) {
		applied <- struct{}{}
	})
	if app.sessionMgr.GetSessionID() != before {
		t.Fatal("the fork was written and applied on the owner loop")
	}
	pump()
	select {
	case <-applied:
	case <-time.After(5 * time.Second):
		t.Fatal("the fork never completed")
	}
	if app.sessionMgr.GetSessionID() == before {
		t.Fatal("the fork did not apply")
	}
}

// An import copies the file (22ms for 53MB) and then parses it, so both run off
// the owner loop.
func TestImportCopiesOffTheOwnerLoop(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	sessionDir := withSessionDir(t, app)
	source := writeImportableSession(t, t.TempDir())
	copied := filepath.Join(sessionDir, filepath.Base(source))
	pump := wireOffLoopSessionWork(t, app)

	applied := make(chan struct{}, 1)
	app.importFromJSONLAsync(context.Background(), source, "", func(*SessionSwitchResult, error) {
		applied <- struct{}{}
	})
	if _, err := os.Stat(copied); err == nil {
		t.Fatal("the import copied and applied on the owner loop")
	}
	pump()
	select {
	case <-applied:
	case <-time.After(5 * time.Second):
		t.Fatal("the import never completed")
	}
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("the import did not copy: %v", err)
	}
}

// Naming a session other than the active one opens its file, so the rename runs
// off the owner loop as well.
func TestRenameOtherSessionOpensOffTheOwnerLoop(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	target := writeImportableSession(t, t.TempDir())
	pump := wireOffLoopSessionWork(t, app)

	errs := make(chan error, 1)
	app.sessions.renameSessionFile(target, "renamed", func(err error) { errs <- err })
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
	pump()
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("rename: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the rename never completed")
	}
}

// Deleting a session forks `trash` and waits for it, so the picker runs it off
// the owner loop and applies the outcome by post.
func TestPickerDeleteRunsOffTheOwnerLoop(t *testing.T) {
	InitTheme("dark", false)
	previous := tui.GetKeybindings()
	tui.SetKeybindings(NewAppKeybindingsManager(nil, "").KeybindingsManager)
	t.Cleanup(func() { tui.SetKeybindings(previous) })
	deleted := ""
	queue := &postQueue{}
	component := NewSessionSelectorComponent(
		func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
			return []coding.SessionInfo{{Path: "/s/a.jsonl", ID: "a"}}, nil
		},
		func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
			return []coding.SessionInfo{{Path: "/s/a.jsonl", ID: "a"}}, nil
		},
		func(string) {}, func() {}, func() {}, func() {},
		SessionSelectorOptions{
			DeleteSession: func(sessionPath string) SessionDeleteResult {
				deleted = sessionPath
				return SessionDeleteResult{OK: true, Method: "trash"}
			},
			ScheduleTimer: func(int, func()) {},
			Post:          queue.Post,
		})
	component.WaitForPendingLoads()
	queue.drain()

	list := component.GetSessionList()
	list.HandleInput("\x04")
	if list.confirmingDeletePath == nil {
		t.Fatalf("confirmation not started; list len=%d selected=%d current=%q",
			len(list.filtered), list.selectedIndex, list.currentSessionCanonicalPath)
	}
	list.HandleInput("\r")
	component.WaitForPendingLoads()
	// The result is applied by the post, so nothing has changed yet.
	if component.GetHeader().statusMessage != nil {
		t.Fatalf("the delete applied before its result reached the loop: %+v", component.GetHeader().statusMessage)
	}
	queue.drain()
	if deleted != "/s/a.jsonl" {
		t.Fatalf("deleted = %q", deleted)
	}
	if message := component.GetHeader().statusMessage; message == nil || message.Message != "Session moved to trash" {
		t.Fatalf("status = %+v", message)
	}
}

// Naming another session opens its file, and the picker only leaves the rename
// once the wiring reports back.
func TestPickerRenameWaitsForItsReport(t *testing.T) {
	InitTheme("dark", false)
	previous := tui.GetKeybindings()
	tui.SetKeybindings(NewAppKeybindingsManager(nil, "").KeybindingsManager)
	t.Cleanup(func() { tui.SetKeybindings(previous) })
	renamed := ""
	queue := &postQueue{}
	component := NewSessionSelectorComponent(
		func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
			return []coding.SessionInfo{{Path: "/s/a.jsonl", ID: "a"}}, nil
		},
		func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
			return []coding.SessionInfo{{Path: "/s/a.jsonl", ID: "a"}}, nil
		},
		func(string) {}, func() {}, func() {}, func() {},
		SessionSelectorOptions{
			RenameSession: func(sessionPath string, currentName string, onDone func(error)) {
				renamed = sessionPath + "=" + currentName
				queue.Post(func() { onDone(nil) })
			},
			ScheduleTimer: func(int, func()) {},
			Post:          queue.Post,
		})
	component.WaitForPendingLoads()
	queue.drain()

	component.enterRenameMode("/s/a.jsonl", "")
	component.confirmRename("renamed")
	if renamed != "/s/a.jsonl=renamed" {
		t.Fatalf("rename call = %q", renamed)
	}
	if component.mode != "rename" {
		t.Fatal("the picker left rename mode before the rename reported")
	}
	queue.drain()
	if component.mode == "rename" {
		t.Fatal("the picker stayed in rename mode after the rename reported")
	}
}
