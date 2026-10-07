package interactive

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/internal/offloop"
	"github.com/dat267/pier/tui"
)

// D200: Windows right-click reads must not block their input callback or
// insert a result after teardown, even for a provider ignoring cancellation.
func TestRightClickPasteIsDetachedFromInputAndShutdown(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	original := clipboardReader
	defer func() { clipboardReader = original }()
	started, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); app.pasteQueue.Flush() }()
	SetClipboardReader(func() (string, error) { close(started); <-release; return "late", nil })
	before := app.defaultEditor.GetText()
	go func() { app.handleRightClickPaste(); close(returned) }()
	<-started
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		unblock()
		<-returned
		t.Fatal("right-click paste blocked the input callback")
	}
	app.StopMode("resume-hint")
	unblock()
	app.pasteQueue.Flush()
	app.ui.RenderNow(false)
	if app.defaultEditor.GetText() != before {
		t.Fatal("right-click paste applied a late result")
	}
}

// D200: fullscreen selection copies must not use the process-wide copier.
func TestSelectionUsesModeOwnedClipboard(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	var copied string
	app.copyText = func(ctx context.Context, text string) error { copied = text; return nil }
	screen := app.newLoopTui(InteractiveTuiOptions{TuiMode: "fullscreen", Terminal: &fakeRendererTerminal{width: 20, height: 6}}).(*tui.AltScreen)
	defer screen.Stop(tui.TuiStopOptions{})
	legacy := false
	original := clipboardCopier
	defer SetClipboardCopier(original)
	SetClipboardCopier(func(string) (bool, string) { legacy = true; return true, "" })
	screen.SetCopyOnSelect(false)
	screen.AddChild(&staticRendererComponent{lines: []string{"hello world"}})
	screen.Start()
	screen.RenderNow(false)
	screen.HandleTerminalInput("\x1b[<0;1;1M")
	screen.HandleTerminalInput("\x1b[<32;6;1M")
	screen.HandleTerminalInput("\x1b[<0;6;1m")
	if !screen.CopyActiveSelectionToClipboard() {
		t.Fatal("selection copy was not handled")
	}
	app.clipboardQueue.Flush()
	if legacy || copied == "" {
		t.Fatal("selection bypassed the mode-owned clipboard")
	}
}

// D200: OAuth URL copies must use the same owned backend as /copy, not the
// component's legacy process-wide fallback.
func TestAuthURLUsesModeOwnedClipboard(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	originalCopy, originalBrowser := authURLCopier, BrowserOpener()
	defer func() { authURLCopier = originalCopy; SetBrowserOpener(originalBrowser) }()
	legacy := false
	authURLCopier = func(string, func(error)) { legacy = true }
	SetBrowserOpener(func(string) {})
	var copied string
	app.copyText = func(ctx context.Context, text string) error { copied = text; return nil }
	app.auth.ShowAmbientAuthDialog(AuthSelectorProvider{ID: "mock", Name: "Mock"})
	dialog := app.editorContainer.Children[0].(*LoginDialogComponent)
	dialog.ShowAuth("https://example.com/auth", "")
	app.ui.RenderNow(false)
	dialog.authUrl.Copy()
	app.clipboardQueue.Flush()
	app.ui.RenderNow(false)
	if legacy || copied != "https://example.com/auth" {
		t.Fatal("auth URL bypassed the mode-owned clipboard")
	}
}

// D200: clipboard copies are owned by their mode, cancel the backend context,
// and do not deliver completion callbacks after that mode has stopped.
func TestStopModeCancelsClipboardCopy(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	started, canceled := make(chan struct{}), make(chan struct{})
	app.copyText = func(ctx context.Context, text string) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}
	completed := false
	app.commands.CopyToClipboard("copy")
	app.copyClipboardAsync("queued", func(error) { completed = true })
	<-started
	app.StopMode("resume-hint")
	<-canceled
	app.clipboardQueue.Flush()
	app.ui.RenderNow(false)
	if completed {
		t.Fatal("clipboard completion escaped the stopped mode")
	}
}

// D200: detached work inherits the mode lifetime even without App.Run.
func TestStopModeCancelsDetachedWork(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	started, canceled, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	app.runDetached(func(ctx context.Context) error {
		defer close(done)
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
		case <-release:
		}
		return nil
	})
	<-started
	app.StopMode("resume-hint")
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("StopMode did not cancel detached work")
	}
	<-done
}

// D200: UI posts queued before shutdown must be gated at execution too.
func TestStopModeRejectsAlreadyPostedOptionalResults(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	before := app.defaultEditor.GetText()
	app.ui.Post(func() { app.defaultEditor.InsertTextAtCursor("late") })
	app.StopMode("resume-hint")
	app.ui.RenderNow(false)
	if app.defaultEditor.GetText() != before {
		t.Fatal("shutdown applied a previously posted optional result")
	}
}

// D199: a theme completion already posted before shutdown must not update
// controller/UI state when the owner eventually drains that callback.
func TestCanceledThemeIgnoresAlreadyPostedCompletion(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	g := offloop.NewGroup()
	q := g.OptionalQueue()
	defer g.StopAll()
	ui := &fakeThemeUI{}
	var posted []func()
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI: ui, ThemeQueue: q,
		Marshal: func(fn func()) { posted = append(posted, fn) },
		Env:     func(string) string { return "" },
	})
	before := controller.ActiveThemeName()
	controller.SetThemeSetting("light")
	q.Flush()
	if len(posted) != 1 {
		t.Fatalf("posted %d completions, want 1", len(posted))
	}
	g.StopAll()
	posted[0]()
	if controller.ActiveThemeName() != before {
		t.Fatal("canceled theme completion changed controller state")
	}
}

// D199: previews have a separate UI completion path from settings switches.
func TestCanceledThemePreviewIgnoresAlreadyPostedCompletion(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	g := offloop.NewGroup()
	q := g.OptionalQueue()
	defer g.StopAll()
	ui := &fakeThemeUI{}
	var posted []func()
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI: ui, ThemeQueue: q,
		Marshal: func(fn func()) { posted = append(posted, fn) },
		Env:     func(string) string { return "" },
	})
	controller.Preview("light")
	q.Flush()
	if len(posted) != 1 {
		t.Fatalf("posted %d previews, want 1", len(posted))
	}
	invalidates, renders := ui.invalidates, ui.renders
	g.StopAll()
	posted[0]()
	if ui.invalidates != invalidates || ui.renders != renders {
		t.Fatal("canceled preview touched the UI")
	}
}

// D199: file reads cannot always be interrupted, so cancellation must be
// checked after loading, before swapping global theme state or posting UI work.
func TestCanceledThemeLoadDoesNotApply(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	light := mustLoadTheme("light")
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	g := offloop.NewGroup()
	q := g.OptionalQueue()
	defer func() { unblock(); g.StopAll(); q.Flush() }()
	posted := 0
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		ThemeQueue: q, Marshal: func(func()) { posted++ },
		Env: func(string) string { return "" },
	})
	controller.loadTheme = func(string, ColorMode) (*Theme, error) { close(started); <-release; return light, nil }
	controller.SetThemeSetting("light")
	<-started
	g.StopAll()
	unblock()
	q.Flush()
	if CurrentThemeName() != "dark" || CurrentTheme() == light {
		t.Fatal("canceled load replaced the global theme")
	}
	if posted != 0 {
		t.Fatal("canceled load posted a completion")
	}
}

// D199: the custom-file watcher must not publish new theme data after its
// queue context is canceled. Timer ticks use virtual time, not wall sleeps.
func TestCanceledThemeWatcherDoesNotReload(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	SetRegisteredThemes(nil)
	StopThemeWatcher()
	original := themeState.onChange.Load()
	themeState.onChange.Store(nil)
	defer themeState.onChange.Store(original)
	writeThemeFile(t, dir, "cancel.json", "cancel", "#ff0000")
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		defer StopThemeWatcher()
		if ok, message := setThemeContext(ctx, "cancel", true, loadTheme); !ok {
			t.Fatal(message)
		}
		synctest.Wait() // watcher has sampled the original modification time
		before := CurrentTheme()
		path := filepath.Join(dir, "cancel.json")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		synctest.Wait()
		writeThemeFile(t, dir, "cancel.json", "cancel", "#00ff00")
		changed := info.ModTime().Add(time.Second)
		if err := os.Chtimes(path, changed, changed); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		synctest.Wait()
		if CurrentTheme() != before {
			t.Fatal("canceled theme watcher published a reload")
		}
	})
}

// D199: a clipboard provider may ignore cancellation. Its late result must
// not insert text into an editor after StopMode, even if posts are drained.
func TestStoppedModeIgnoresLateClipboardPaste(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	original := clipboardReader
	defer func() { clipboardReader = original }()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); app.pasteQueue.Flush() }()
	SetClipboardReader(func() (string, error) { close(started); <-release; return "late paste", nil })
	before := app.defaultEditor.GetText()
	app.key.OnPasteImage()
	<-started
	app.StopMode("resume-hint")
	unblock()
	app.pasteQueue.Flush()
	app.ui.RenderNow(false)
	if app.defaultEditor.GetText() != before {
		t.Fatal("late clipboard result changed the stopped editor")
	}
}

type blockedShutdownPreparer struct {
	*tui.Text
	started, release chan struct{}
}

func (p *blockedShutdownPreparer) Prepare(width int) {
	close(p.started)
	<-p.release
}

// D199: a detached warm that finishes after cancellation must not publish a
// ready chunk or wake a renderer which has already been torn down.
func TestCanceledPrerenderDoesNotPublishLateResult(t *testing.T) {
	g := offloop.NewGroup()
	q := g.OptionalQueue()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); g.StopAll(); q.Flush() }()
	screen := tui.NewMainScreen(&fakeRendererTerminal{width: 80, height: 24}, false, "")
	r := NewTranscriptRenderer(&tui.Container{}, screen, nil, nil, nil)
	r.PrerenderQueue = q
	r.deferredComponents = []tui.Component{&blockedShutdownPreparer{tui.NewText("late", 0, 0, nil), started, release}}
	r.MaterializeDeferred(80)
	<-started
	g.StopAll()
	unblock()
	q.Flush()
	if r.pre.ready {
		t.Fatal("canceled worker published a ready transcript chunk")
	}
	select {
	case <-screen.RenderTicks():
		t.Fatal("canceled worker woke the renderer")
	default:
	}
	if len(r.Chat.Children) != 0 {
		t.Fatal("canceled worker attached transcript content")
	}
}
