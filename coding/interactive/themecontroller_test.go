package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/internal/offloop"
	"time"

	"github.com/dat267/pier/tui"
)

// ---- Chat viewport ----

type staticLines struct {
	lines []string
}

// TestChatViewportLayout verifies the viewport composition.
func TestChatViewportLayout(t *testing.T) {
	document := &staticLines{lines: []string{"doc"}}
	pending := &staticLines{lines: []string{"pending"}}
	status := &staticLines{lines: []string{"status"}}
	editor := &staticLines{lines: []string{"editor"}}
	footer := &staticLines{lines: []string{"footer"}}
	above := &staticLines{lines: []string{"above"}}
	below := &staticLines{lines: []string{"below"}}

	viewport := CreateChatViewport(ChatViewportOptions{
		Document: document, PendingMessages: pending, Status: status, Editor: editor, Footer: footer,
		WidgetsAbove: above, WidgetsBelow: below,
	})
	if viewport.Root == nil || viewport.Transcript == nil {
		t.Fatal("viewport not composed")
	}
	if !viewport.Transcript.Primary() || viewport.Transcript.Overscroll() != "chain" {
		t.Fatalf("transcript = primary:%v overscroll:%q", viewport.Transcript.Primary(), viewport.Transcript.Overscroll())
	}
	if viewport.Transcript.Scrollbar() != tui.ScrollbarAuto {
		t.Fatalf("scrollbar = %q", viewport.Transcript.Scrollbar())
	}

	frame := tui.RenderLayoutFrame(viewport.Root, 20, 12, func() {})
	rendered := strings.Join(frame.Lines, "\n")
	for _, expected := range []string{"doc", "pending", "status", "above", "editor", "below", "footer"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("missing %q in:\n%s", expected, rendered)
		}
	}

	// Without widgets the dock still renders.
	plain := CreateChatViewport(ChatViewportOptions{
		Document: document, PendingMessages: pending, Status: status, Editor: editor, Footer: footer,
		Scrollbar: tui.ScrollbarHidden,
	})
	if plain.Transcript.Scrollbar() != tui.ScrollbarHidden {
		t.Fatalf("scrollbar = %q", plain.Transcript.Scrollbar())
	}
	plainFrame := tui.RenderLayoutFrame(plain.Root, 20, 8, func() {})
	if joined := strings.Join(plainFrame.Lines, "\n"); strings.Contains(joined, "above") {
		t.Fatalf("unexpected widget:\n%s", joined)
	}
}

// TestThemeControllerProbesSystemColors pins that the controller stores the
// terminal's reported colors and rebuilds the active system theme.
func TestThemeControllerProbesSystemColors(t *testing.T) {
	t.Cleanup(func() { SetSystemTerminalColors(tui.TerminalColors{}) })
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)

	ui := &fakeThemeUI{}
	background := tui.RgbColor{R: 0x28, G: 0x2a, B: 0x36}
	foreground := tui.RgbColor{R: 0xf8, G: 0xf8, B: 0xf2}
	ui.terminalColors = tui.TerminalColors{Background: &background, Foreground: &foreground}
	settings := &fakeThemeSettings{}
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:                 ui,
		GetSettingsManager: func() ThemeSettings { return settings },
		TimeoutMS:          1,
		Env:                func(key string) string { return "" },
	})
	if result := controller.SetThemeName(SystemThemeName, false); !result.Success {
		t.Fatalf("system theme did not load: %+v", result)
	}
	controller.ProbeSystemColors()
	if ui.systemProbes == 0 {
		t.Fatal("terminal colors were not queried")
	}
	if got := CurrentTheme().GetFgAnsi("text"); got != "\x1b[39m" {
		t.Fatalf("system text ansi = %q", got)
	}
}

// TestProbeSystemColorsDoesNotBlockTheCaller pins the fix for the startup stall:
// the color query must run off the owner loop, because the terminal's reply is
// dispatched on that loop. A blocking query can never observe the reply and so
// always waits out the full timeout, delaying the first frame.
func TestProbeSystemColorsDoesNotBlockTheCaller(t *testing.T) {
	t.Cleanup(func() { SetSystemTerminalColors(tui.TerminalColors{}) })
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)

	gate := make(chan struct{})
	started := make(chan struct{})
	applied := make(chan struct{}, 1)
	ui := &fakeThemeUI{queryGate: gate, queryStarted: started}
	background := tui.RgbColor{R: 0x28, G: 0x2a, B: 0x36}
	ui.terminalColors = tui.TerminalColors{Background: &background}
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:  ui,
		Env: func(key string) string { return "" },
		Marshal: func(fn func()) {
			fn()
			select {
			case applied <- struct{}{}:
			default:
			}
		},
	})

	done := make(chan struct{})
	go func() {
		controller.ProbeSystemColors()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(gate)
		t.Fatal("ProbeSystemColors blocked the caller")
	}

	<-started
	close(gate)
	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("settled terminal colors were not applied")
	}
}

func (f *fakeThemeUI) emitBackground(color tui.RgbColor) {
	f.mu.Lock()
	listener := f.bgListener
	f.mu.Unlock()
	if listener != nil {
		listener(color)
	}
}

func (f *fakeThemeUI) emit(theme TerminalTheme) {
	f.mu.Lock()
	listener := f.listener
	f.mu.Unlock()
	if listener != nil {
		listener(theme)
	}
}

type fakeThemeSettings struct {
	setting  *string
	setCalls []string
	flushes  int
}

// TestThemeControllerLifecycle verifies the controller's settings-driven flow.
func TestThemeControllerLifecycle(t *testing.T) {
	SetSystemTerminalColors(tui.TerminalColors{})
	SetSystemTerminalColorScheme(nil)
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		SetSystemTerminalColorScheme(nil)
	})
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	SetRegisteredThemes(nil)

	ui := &fakeThemeUI{}
	settings := &fakeThemeSettings{}
	errors := []string{}
	changed := 0

	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:                 ui,
		GetSettingsManager: func() ThemeSettings { return settings },
		ShowError:          func(message string) { errors = append(errors, message) },
		OnChanged:          func() { changed++ },
		TimeoutMS:          1,
		Env:                func(key string) string { return "" },
	})
	if CurrentThemeName() == "" {
		t.Fatal("no theme initialized")
	}
	if controller.GetTerminalTheme() != TerminalThemeDark {
		t.Fatalf("terminal theme = %q", controller.GetTerminalTheme())
	}

	// Selecting a theme by name persists the selection and notifies.
	result := controller.SetThemeName("light", true)
	if !result.Success || controller.ActiveThemeName() != "light" {
		t.Fatalf("set theme = %+v active=%q", result, controller.ActiveThemeName())
	}
	if changed == 0 || ui.invalidates == 0 {
		t.Fatal("change not propagated")
	}
	if selection := controller.GetThemeSelection(); selection != "light" {
		t.Fatalf("selection = %q", selection)
	}

	// pi v1.4.0 theme-controller.ts applyThemeName reports the error and
	// falls back to the system theme.
	result = controller.SetThemeName("missing", true)
	if result.Success {
		t.Fatal("missing theme accepted")
	}
	if controller.ActiveThemeName() != SystemThemeName {
		t.Fatalf("active = %q", controller.ActiveThemeName())
	}
	if len(errors) != 1 || !strings.Contains(errors[0], "Fell back to the system theme.") {
		t.Fatalf("errors = %v", errors)
	}

	// Auto settings sync with the terminal color scheme.
	light := "light"
	dark := "dark"
	setting := light + "/" + dark
	controller.SetThemeSetting(setting)
	if controller.GetThemeSelection() != setting {
		t.Fatalf("selection = %q", controller.GetThemeSelection())
	}
	ui.emit(TerminalThemeLight)
	if controller.ActiveThemeName() != "light" {
		t.Fatalf("auto light = %q", controller.ActiveThemeName())
	}
	ui.emit(TerminalThemeDark)
	if controller.ActiveThemeName() != "dark" {
		t.Fatalf("auto dark = %q", controller.ActiveThemeName())
	}
	ui.mu.Lock()
	notifications := ui.notifications
	ui.mu.Unlock()
	if !notifications {
		t.Fatal("auto-sync notifications not enabled")
	}

	// The proactive OSC 11 probe runs once and its reply drives auto-sync.
	controller.ProbeTerminalBackground()
	controller.ProbeTerminalBackground()
	ui.mu.Lock()
	probes := ui.bgProbes
	ui.mu.Unlock()
	if probes != 1 {
		t.Fatalf("background probes = %d", probes)
	}
	ui.emitBackground(tui.RgbColor{R: 255, G: 255, B: 255})
	if controller.ActiveThemeName() != "light" {
		t.Fatalf("auto background light = %q", controller.ActiveThemeName())
	}
	ui.emitBackground(tui.RgbColor{R: 0, G: 0, B: 0})
	if controller.ActiveThemeName() != "dark" {
		t.Fatalf("auto background dark = %q", controller.ActiveThemeName())
	}

	// Preview switches without persisting.
	controller.Preview("light")
	if CurrentThemeName() != "light" {
		t.Fatalf("preview = %q", CurrentThemeName())
	}

	// In-memory instances.
	instance := CreateTheme(getBuiltinThemes()["dark"], ColorModeTruecolor, "")
	if result := controller.SetThemeInstance(instance); !result.Success || controller.ActiveThemeName() != "<in-memory>" {
		t.Fatalf("instance = %+v %q", result, controller.ActiveThemeName())
	}

	controller.DisableAutoSync()
	ui.mu.Lock()
	notifications = ui.notifications
	ui.mu.Unlock()
	if notifications {
		t.Fatal("notifications still enabled")
	}
	controller.Dispose()
}

// TestThemeControllerUsesSystemThemeWhenUnset ports v1.2.0
// interactive-mode.ts/theme-controller.ts: without a theme setting,
// applyFromSettings selects system and does not persist
// an environment-derived light/dark theme.
func TestThemeControllerUsesSystemThemeWhenUnset(t *testing.T) {
	SetSystemTerminalColors(tui.TerminalColors{})
	SetSystemTerminalColorScheme(nil)
	t.Setenv("COLORFGBG", "")
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		SetSystemTerminalColorScheme(nil)
	})
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	ui := &fakeThemeUI{}
	settings := &fakeThemeSettings{}
	env := map[string]string{"COLORFGBG": "0;15"}
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:                 ui,
		GetSettingsManager: func() ThemeSettings { return settings },
		TimeoutMS:          1,
		Env:                func(key string) string { return env[key] },
	})
	if controller.GetTerminalTheme() != TerminalThemeLight {
		t.Fatalf("terminal theme = %q", controller.GetTerminalTheme())
	}
	if controller.ActiveThemeName() != SystemThemeName {
		t.Fatalf("initial active theme = %q, want %q", controller.ActiveThemeName(), SystemThemeName)
	}

	controller.ApplyFromSettings()
	if controller.ActiveThemeName() != SystemThemeName {
		t.Fatalf("active theme = %q, want %q", controller.ActiveThemeName(), SystemThemeName)
	}
	if len(settings.setCalls) != 0 || settings.flushes != 0 {
		t.Fatalf("unset theme was persisted: set calls=%v flushes=%d", settings.setCalls, settings.flushes)
	}
	ui.mu.Lock()
	notifications := ui.notifications
	ui.mu.Unlock()
	if !notifications {
		t.Fatal("system theme did not enable terminal appearance notifications")
	}
	ui.mu.Lock()
	listener := ui.listener
	ui.mu.Unlock()
	if listener == nil {
		t.Fatal("system theme did not register a terminal appearance listener")
	}
	listener(TerminalThemeLight)
	if controller.GetTerminalTheme() != TerminalThemeLight {
		t.Fatalf("terminal theme after appearance report = %q, want light", controller.GetTerminalTheme())
	}
	if got := CurrentTheme().Appearance(); got != string(TerminalThemeLight) {
		t.Fatalf("system theme appearance after report = %q, want light", got)
	}
	darkBackground := tui.RgbColor{R: 0, G: 0, B: 0}
	SetSystemTerminalColors(tui.TerminalColors{Background: &darkBackground})
	if got := CurrentTheme().Appearance(); got != string(TerminalThemeDark) {
		t.Fatalf("reported background did not override the scheme: appearance = %q", got)
	}
	if controller.ActiveThemeName() != SystemThemeName {
		t.Fatalf("active theme after appearance report = %q, want %q", controller.ActiveThemeName(), SystemThemeName)
	}
}

// TestSystemThemeRefreshesTerminalColorsOnAppearanceChange ports v1.2.0
// theme-controller.ts applyTerminalColorSchemeChange, which re-queries colors
// after each report.
func TestSystemThemeRefreshesTerminalColorsOnAppearanceChange(t *testing.T) {
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		SetSystemTerminalColorScheme(nil)
	})
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	background := tui.RgbColor{R: 255, G: 255, B: 255}
	ui := &fakeThemeUI{terminalColors: tui.TerminalColors{Background: &background}}
	setting := SystemThemeName
	applied := make(chan struct{}, 2)
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:                  ui,
		InitialThemeSetting: &setting,
		Env:                 func(string) string { return "" },
		Marshal: func(fn func()) {
			fn()
			applied <- struct{}{}
		},
	})
	defer controller.Dispose()

	controller.ApplyFromSettings()
	controller.ProbeSystemColors()
	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("initial system-color query did not apply")
	}
	ui.mu.Lock()
	listener := ui.listener
	initialQueries := ui.systemProbes
	ui.mu.Unlock()
	if listener == nil {
		t.Fatal("system theme did not register a terminal appearance listener")
	}

	listener(TerminalThemeDark)
	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal appearance report did not refresh system colors")
	}
	ui.mu.Lock()
	queries := ui.systemProbes
	ui.mu.Unlock()
	if queries != initialQueries+1 {
		t.Fatalf("system-color queries after appearance change = %d, want %d", queries, initialQueries+1)
	}
}

// TestAutoThemeSettingQueriesCurrentColors ports v1.2.0
// theme-controller.ts applyFromSettings: changing to a pair after startup
// chooses its current half without waiting for the next appearance notification.
func TestAutoThemeSettingQueriesCurrentTerminalColors(t *testing.T) {
	t.Cleanup(func() { SetSystemTerminalColors(tui.TerminalColors{}) })
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	background := tui.RgbColor{R: 255, G: 255, B: 255}
	ui := &fakeThemeUI{terminalColors: tui.TerminalColors{Background: &background}}
	setting := "dark"
	applied := make(chan struct{}, 2)
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:                  ui,
		InitialThemeSetting: &setting,
		Env:                 func(string) string { return "" },
		Marshal: func(fn func()) {
			fn()
			applied <- struct{}{}
		},
	})
	defer controller.Dispose()
	controller.ApplyFromSettings()
	controller.ProbeSystemColors()
	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("initial system-color query did not apply")
	}

	controller.SetThemeSetting("light/dark")
	ui.mu.Lock()
	queries := ui.systemProbes
	ui.mu.Unlock()
	if queries != 2 {
		t.Fatalf("system-color queries after selecting auto theme = %d, want 2", queries)
	}
	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("auto-theme selection did not apply current terminal colors")
	}
	if controller.ActiveThemeName() != "light" {
		t.Fatalf("active auto theme = %q, want light", controller.ActiveThemeName())
	}
}

// ---- Detection helpers ----

type fakeDetector struct {
	rgb         RgbColor
	hasRGB      bool
	scheme      TerminalTheme
	hasScheme   bool
	rgbCalls    int
	schemeCalls int
}

// TestTerminalDetection verifies the query-based detection helpers.
func TestTerminalDetection(t *testing.T) {
	detector := &fakeDetector{rgb: RgbColor{R: 250, G: 250, B: 250}, hasRGB: true}
	detection := DetectTerminalBackgroundTheme(detector, 1, func(string) string { return "" })
	if detection.Theme != TerminalThemeLight || detection.Source != "terminal background" {
		t.Fatalf("detection = %+v", detection)
	}
	if detection.Detail != "OSC 11 background rgb(250, 250, 250)" {
		t.Fatalf("detail = %q", detection.Detail)
	}

	// A missing background falls back to the environment.
	detector = &fakeDetector{}
	detection = DetectTerminalBackgroundTheme(detector, 1, func(key string) string {
		if key == "COLORFGBG" {
			return "0;15"
		}
		return ""
	})
	if detection.Source != "COLORFGBG" || detection.Theme != TerminalThemeLight {
		t.Fatalf("detection = %+v", detection)
	}

	// The color-scheme report wins for auto detection.
	detector = &fakeDetector{scheme: TerminalThemeDark, hasScheme: true, rgb: RgbColor{R: 255, G: 255, B: 255}, hasRGB: true}
	if theme := DetectTerminalThemeForAuto(detector, 1, func(string) string { return "" }); theme != TerminalThemeDark {
		t.Fatalf("auto theme = %q", theme)
	}
}

// With a theme queue wired (the interactive wiring), named-theme switches and
// previews load off the calling goroutine and apply in submission order; the
// controller state update marshals back through the Marshal seam.
func TestThemeSettingAppliesOnTheQueue(t *testing.T) {
	ui := &fakeThemeUI{}
	var marshaled int
	queue := offloop.New()
	defer queue.Stop()
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:         ui,
		TimeoutMS:  1,
		Env:        func(string) string { return "" },
		Marshal:    func(fn func()) { marshaled++; fn() },
		ThemeQueue: queue,
	})
	defer controller.Dispose()
	controller.SetThemeSetting("light")
	controller.ThemeQueueFlushForTest() // observe this accepted load before the next selection
	controller.SetThemeSetting("dark")
	controller.ThemeQueueFlushForTest()
	if CurrentThemeName() != "dark" {
		t.Fatalf("current theme = %q, want dark (last switch wins)", CurrentThemeName())
	}
	if controller.ActiveThemeName() != "dark" {
		t.Fatalf("active theme = %q", controller.ActiveThemeName())
	}
	if marshaled != 2 {
		t.Fatalf("marshaled = %d, want 2", marshaled)
	}
	controller.Preview("light")
	controller.ThemeQueueFlushForTest()
	if CurrentThemeName() != "light" {
		t.Fatalf("preview = %q", CurrentThemeName())
	}
}

// TestThemeControllerDoesNotProbeWithoutAutoSync pins that the OSC 11 probe is
// only written when an auto (light/dark) theme is active: a fixed theme has no
// use for the terminal background.
func TestThemeControllerDoesNotProbeWithoutAutoSync(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	SetRegisteredThemes(nil)

	ui := &fakeThemeUI{}
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI:        ui,
		TimeoutMS: 1,
		Env:       func(string) string { return "" },
	})
	controller.SetThemeName("dark", false)
	controller.ProbeTerminalBackground()
	ui.mu.Lock()
	probes := ui.bgProbes
	ui.mu.Unlock()
	if probes != 0 {
		t.Fatalf("probed with auto-sync off: %d", probes)
	}
}
