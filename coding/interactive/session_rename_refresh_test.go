package interactive

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/offloop"
	"github.com/dat267/pier/tui"
)

// pi f9a26365 interactive-mode.ts persists the rename synchronously before
// session-selector.ts refreshSessionsAfterMutation reloads the session list.
// The port's background listing must not overtake its queued rename write.
func TestSessionSelectorRefreshWaitsForQueuedRename(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	keys := NewAppKeybindingsManager(nil, "")
	previous := tui.GetKeybindings()
	tui.SetKeybindings(keys.KeybindingsManager)
	defer tui.SetKeybindings(previous)

	dir := t.TempDir()
	queue := offloop.New()
	defer queue.Stop()
	manager := coding.NewSessionManager(dir, &coding.SessionManagerOptions{SessionDir: dir, WriteQueue: queue})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first prompt"}})
	manager.AppendSessionInfo("old name")
	queue.Flush()

	screen := tui.NewMainScreen(&fakeRendererTerminal{width: 80, height: 24}, false, "")
	editor := tui.NewText("", 0, 0, nil)
	wiring := &SessionWiring{SessionInfo: manager, UI: screen,
		Slot: NewSelectorSlot(screen, &tui.Container{}, editor), Keybindings: keys.KeybindingsManager}
	wiring.ShowSessionSelector()
	selector := wiring.Slot.ActiveSelectorComponent().(*SessionSelectorComponent)
	selector.WaitForPendingLoads()
	screen.RenderNow(false)
	if !strings.Contains(strings.Join(selector.Render(80), "\n"), "old name") {
		t.Fatal("initial session list did not load")
	}

	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	queue.Go(func() { <-release })
	selector.HandleInput("\x12") // ctrl+r opens rename for the selected session.
	selector.renameInput.SetValue("new name")
	selector.HandleInput("\r")
	loaded := make(chan struct{})
	go func() { selector.WaitForPendingLoads(); close(loaded) }()
	select {
	case <-loaded:
		screen.RenderNow(false)
		if got := strings.Join(selector.Render(80), "\n"); !strings.Contains(got, "new name") {
			t.Errorf("rename refresh overtook persistence and restored the old name: %q", got)
		}
	case <-time.After(100 * time.Millisecond):
		// Correctly waiting for the controlled write barrier, not blocking UI.
	}
	unblock()
	select {
	case <-loaded:
	case <-time.After(5 * time.Second):
		t.Fatal("session reload did not finish after the write barrier released")
	}
	screen.RenderNow(false)
	if got := strings.Join(selector.Render(80), "\n"); !strings.Contains(got, "new name") {
		t.Errorf("reloaded list has no renamed session: %q", got)
	}
}
