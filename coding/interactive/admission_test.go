package interactive

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// D201: owner-loop input still paints while a clipboard daemon is blocked
// and a burst fills admission. All editor observations are posted to the owner.
func TestInputPaintsDuringBlockedClipboardBurst(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	app.copyText = func(ctx context.Context, text string) error {
		if text == "0" {
			close(started)
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}
	stop := startLoopApp(t, app)
	defer stop()
	defer unblock()
	burst := make(chan struct{})
	app.ui.Post(func() {
		for i := 0; i < 65; i++ {
			app.commands.CopyToClipboard(strconv.Itoa(i))
		}
		close(burst)
	})
	select {
	case <-burst:
	case <-time.After(2 * time.Second):
		t.Fatal("submission burst blocked the owner loop")
	}
	<-started
	queued, running := app.clipboardQueue.Backlog()
	if queued != 3 || running != 1 {
		t.Fatalf("loop copy backlog = (%d,%d), want (3,1)", queued, running)
	}
	app.postTerminalInput("responsive")
	waitForConditionWithin(t, func() bool { return strings.Contains(editorText(app), "responsive") }, 2*time.Second)
}

// D201: a rejected reload must undo its temporary loading screen, rather
// than leaving the editor unavailable when no worker was admitted.
func TestRejectedReloadRestoresEditor(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	release := make(chan struct{})
	var workers sync.WaitGroup
	defer func() { close(release); workers.Wait() }()
	for i := 0; i < 4; i++ {
		workers.Add(1)
		app.runDetached(func(context.Context) error { defer workers.Done(); <-release; return nil })
	}
	app.commands.HandleReloadCommand()
	app.ui.RenderNow(false)
	if len(app.editorContainer.Children) != 1 || app.editorContainer.Children[0] != app.defaultEditor || app.ui.GetFocusedComponent() != app.defaultEditor {
		t.Fatal("rejected reload left the editor replaced by its loading screen")
	}
}

// D201: detached jobs have bounded parallel admission, not an unbounded
// goroutine per request. Rejected jobs do not run and show visible feedback.
func TestDetachedAdmissionBoundsBurstAndReportsBusy(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	admission, ok := any(app).(interface {
		runDetached(func(context.Context) error) bool
	})
	if !ok {
		t.Fatal("detached work does not expose admission")
	}
	release := make(chan struct{})
	var workers sync.WaitGroup
	defer func() { close(release); workers.Wait() }()
	accepted := 0
	for i := 0; i < 65; i++ {
		workers.Add(1)
		if admission.runDetached(func(context.Context) error { defer workers.Done(); <-release; return nil }) {
			accepted++
		} else {
			workers.Done()
		}
	}
	if accepted != 4 {
		t.Fatalf("detached admission = %d, want 4", accepted)
	}
	app.ui.RenderNow(false)
	found := false
	for _, line := range app.chat.Render(80) {
		if strings.Contains(line, "Background work busy") {
			found = true
		}
	}
	if !found {
		t.Fatal("detached rejection had no visible feedback")
	}
	if count := strings.Count(strings.Join(app.chat.Render(80), "\n"), "Background work busy"); count != 1 {
		t.Fatalf("burst created %d busy warnings, want one coalesced notification", count)
	}
}

// D201: repeated copies cannot accumulate behind a stalled clipboard daemon.
// Accepted distinct copies remain FIFO; rejected commands report busy status.
func TestClipboardAdmissionBoundsBurstAndReportsBusy(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	release, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); app.clipboardQueue.Flush() }()
	var copied []string
	app.copyText = func(ctx context.Context, text string) error {
		if text == "0" {
			close(started)
			<-release
		}
		copied = append(copied, text)
		return nil
	}
	if ok, _ := app.commands.CopyToClipboard("0"); !ok {
		t.Fatal("first copy rejected")
	}
	<-started
	accepted := 1
	for i := 1; i <= 64; i++ {
		ok, message := app.commands.CopyToClipboard(strconv.Itoa(i))
		if ok {
			accepted++
		} else if message != "Clipboard busy; try again after pending copies finish." {
			t.Fatalf("rejection feedback = %q", message)
		}
	}
	queued, running := app.clipboardQueue.Backlog()
	t.Logf("64 extra copies: accepted=%d queued=%d running=%d", accepted, queued, running)
	if accepted != 4 || queued != 3 || running != 1 {
		t.Fatalf("accepted = %d, backlog = (%d,%d), want 4 and (3,1)", accepted, queued, running)
	}
	unblock()
	app.clipboardQueue.Flush()
	if !reflect.DeepEqual(copied, []string{"0", "1", "2", "3"}) {
		t.Fatalf("copy FIFO = %v", copied)
	}
}

// D201: a stalled load plus repeated previews/settings changes retains only
// the running load and the newest successor, regardless of burst size.
func TestThemeAdmissionKeepsOnlyNewestPendingSelection(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	release, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); app.theme.ThemeQueueFlushForTest() }()
	var loaded []string
	app.theme.loadTheme = func(name string, mode ColorMode) (*Theme, error) {
		loaded = append(loaded, name)
		if len(loaded) == 1 {
			close(started)
			<-release
		}
		return loadTheme(name, mode)
	}
	app.theme.Preview("light")
	<-started
	for i := 0; i < 64; i++ {
		app.theme.Preview("light")
	}
	app.theme.SetThemeSetting("dark")
	queued, running := app.theme.themeQueue.Backlog()
	t.Logf("65 redundant/pending selections: queued=%d running=%d", queued, running)
	if queued != 1 || running != 1 {
		t.Fatalf("theme backlog = (%d,%d), want (1,1)", queued, running)
	}
	unblock()
	app.theme.ThemeQueueFlushForTest()
	app.ui.RenderNow(false)
	if !reflect.DeepEqual(loaded, []string{"light", "dark"}) || CurrentThemeName() != "dark" {
		t.Fatalf("loaded = %v, current = %s", loaded, CurrentThemeName())
	}
}
