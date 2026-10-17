package interactive

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

// recordingTerminal records writes so the theme probe tests can assert what was
// sent to the terminal.
type recordingTerminal struct {
	*fakeRendererTerminal
	mu     sync.Mutex
	writes []string
}

func (r *recordingTerminal) Write(data string) {
	r.mu.Lock()
	r.writes = append(r.writes, data)
	r.mu.Unlock()
}

func (r *recordingTerminal) hasWrite(sub string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, write := range r.writes {
		if strings.Contains(write, sub) {
			return true
		}
	}
	return false
}

func (r *recordingTerminal) joinedWrites() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.writes, "")
}

// TestBackgroundProbeRoundTripThroughRenderer drives the real renderer through
// the safe detection path: the color-scheme notification toggle is deferred
// until Start, the OSC 11 probe writes the query without blocking, and a reply
// switches the auto theme. This is the end-to-end shape of the app wiring.
func TestBackgroundProbeRoundTripThroughRenderer(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	SetRegisteredThemes(nil)

	terminal := &recordingTerminal{fakeRendererTerminal: &fakeRendererTerminal{width: 80, height: 24}}
	screen := tui.NewMainScreen(terminal, false, "")
	setting := "light/dark"
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:                  themeUIAdapter{ui: screen},
		InitialThemeSetting: &setting,
		TimeoutMS:           1,
		Env:                 func(key string) string { return "" },
	})
	controller.ApplyFromSettings()

	if terminal.hasWrite("\x1b[?2031h") {
		t.Fatal("color-scheme notification written before Start")
	}
	screen.Start()
	if !terminal.hasWrite("\x1b[?2031h") {
		t.Fatal("Start did not replay the color-scheme notification")
	}

	controller.ProbeTerminalBackground()
	if !terminal.hasWrite("\x1b]11;?") {
		t.Fatal("probe did not write the OSC 11 query")
	}
	if controller.ActiveThemeName() != "dark" {
		t.Fatalf("active before reply = %q", controller.ActiveThemeName())
	}

	screen.HandleTerminalInput("\x1b]11;#ffffff\x07")
	if controller.ActiveThemeName() != "light" {
		t.Fatalf("active after light reply = %q", controller.ActiveThemeName())
	}
	screen.HandleTerminalInput("\x1b]11;#000000\x07")
	if controller.ActiveThemeName() != "dark" {
		t.Fatalf("active after dark reply = %q", controller.ActiveThemeName())
	}
	screen.Stop(tui.TuiStopOptions{})
}

// TestBackgroundListenerSeesLateReplyDuringColorQuery verifies that the
// combined color query and the standalone OSC 11 listener both receive the
// background response, even when the slow query timed out before it arrived.
func TestBackgroundListenerSeesLateReplyDuringColorQuery(t *testing.T) {
	t.Cleanup(func() { SetSystemTerminalColors(tui.TerminalColors{}) })
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)

	terminal := &recordingTerminal{fakeRendererTerminal: &fakeRendererTerminal{width: 80, height: 24}}
	screen := tui.NewMainScreen(terminal, false, "")
	setting := "light/dark"
	marshaled := make(chan func(), 1)
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:                  themeUIAdapter{ui: screen},
		InitialThemeSetting: &setting,
		TimeoutMS:           1,
		Env:                 func(string) string { return "" },
		Marshal:             func(fn func()) { marshaled <- fn },
	})
	defer controller.Dispose()
	defer screen.Stop(tui.TuiStopOptions{})

	controller.ApplyFromSettings()
	screen.Start()
	controller.ProbeTerminalBackground()
	controller.ProbeSystemColors()
	select {
	case apply := <-marshaled:
		apply()
	case <-time.After(2 * time.Second):
		t.Fatal("system-color query did not apply its timeout fallback")
	}

	// The late OSC 11 response alone must update the auto theme. No palette
	// replies or DA1 sentinel are available on this deliberately slow path.
	screen.HandleTerminalInput("\x1b]11;#ffffff\x07")
	if controller.ActiveThemeName() != "light" {
		t.Fatalf("theme after late OSC 11 reply = %q, want light", controller.ActiveThemeName())
	}
}

// TestSystemColorQueryDoesNotHideStartupBackgroundReply covers the actual
// startup ordering: the OSC 11 auto-theme probe is followed by the bundled
// OSC 10/11/4 system-theme query. Upstream always applies the background reply
// to light/dark selection, including when the combined query is outstanding
// (`pi/packages/coding-agent/src/modes/interactive/theme/theme-controller.ts`).
func TestSystemColorQueryDoesNotHideStartupBackgroundReply(t *testing.T) {
	t.Cleanup(func() { SetSystemTerminalColors(tui.TerminalColors{}) })
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)

	terminal := &recordingTerminal{fakeRendererTerminal: &fakeRendererTerminal{width: 80, height: 24}}
	screen := tui.NewMainScreen(terminal, false, "")
	setting := "light/dark"
	marshaled := make(chan func(), 1)
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:                  themeUIAdapter{ui: screen},
		InitialThemeSetting: &setting,
		TimeoutMS:           1,
		Env:                 func(string) string { return "" },
		Marshal:             func(fn func()) { marshaled <- fn },
	})
	defer controller.Dispose()
	defer screen.Stop(tui.TuiStopOptions{})

	controller.ApplyFromSettings()
	screen.Start()
	controller.ProbeTerminalBackground()
	controller.ProbeSystemColors()
	if controller.ActiveThemeName() != "dark" {
		t.Fatalf("initial fallback theme = %q, want dark", controller.ActiveThemeName())
	}

	// Let the short query timeout apply its fallback before delivering a slow
	// SSH-like response.
	select {
	case apply := <-marshaled:
		apply()
	case <-time.After(2 * time.Second):
		t.Fatal("system-color query did not apply its timeout fallback")
	}
	screen.HandleTerminalInput("\x1b]11;#ffffff\x07")
	// The DA1 sentinel settles the late bundled query. Its result must update
	// the light/dark pair even though the bundled query consumed OSC 11 before
	// the standalone background listener could see it.
	screen.HandleTerminalInput("\x1b[?62;22c")
	if controller.ActiveThemeName() != "light" {
		t.Fatalf("theme after late terminal background reply = %q, want light", controller.ActiveThemeName())
	}
}

// TestStaleSystemColorSnapshotDoesNotClobberLateBackground covers the ordering
// where the combined color query times out first and a late OSC 11 background
// reply arrives before the loop drains the timeout apply. The timeout snapshot
// is empty (or partial); applying it after the late reply must not overwrite the
// terminal's real background, which the system theme is generated from.
func TestStaleSystemColorSnapshotDoesNotClobberLateBackground(t *testing.T) {
	t.Cleanup(func() { SetSystemTerminalColors(tui.TerminalColors{}) })
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)

	terminal := &recordingTerminal{fakeRendererTerminal: &fakeRendererTerminal{width: 80, height: 24}}
	screen := tui.NewMainScreen(terminal, false, "")
	setting := SystemThemeName
	marshaled := make(chan func(), 4)
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:                  themeUIAdapter{ui: screen},
		InitialThemeSetting: &setting,
		TimeoutMS:           1,
		Env:                 func(string) string { return "" },
		Marshal:             func(fn func()) { marshaled <- fn },
	})
	defer controller.Dispose()
	defer screen.Stop(tui.TuiStopOptions{})

	controller.ApplyFromSettings()
	screen.Start()
	controller.ProbeTerminalBackground()
	controller.ProbeSystemColors()

	// The combined query times out and posts its stale snapshot.
	var stale func()
	select {
	case stale = <-marshaled:
	case <-time.After(2 * time.Second):
		t.Fatal("system-color query did not post its timeout fallback")
	}

	// The terminal answers late with a light background. Only the background
	// listener sees it; the combined query is still pending.
	screen.HandleTerminalInput("\x1b]11;#ffffff\x07")
	if colors := systemThemeTerminalColors(); colors == nil || colors.Background == nil {
		t.Fatal("late background reply was not recorded")
	}

	// The loop drains the stale timeout apply after the input reply.
	stale()
	colors := systemThemeTerminalColors()
	if colors == nil || colors.Background == nil {
		t.Fatalf("stale timeout snapshot clobbered the late background: %+v", colors)
	}
	if got := getTerminalTheme(); got != "light" {
		t.Fatalf("system theme detected %q after the late background, want light", got)
	}
}
