package interactive

import (
	"testing"

	"github.com/dat267/pier/tui"
)

// pi v1.4.0 theme-controller.ts applyTerminalColors/reapplyForTerminal uses
// theme.ts detectTerminalTheme, including the reported foreground. At #757575,
// both black and white meet 4.5:1; black foreground makes the appearance light
// even though white has slightly higher contrast against the background alone.
func TestThemeControllerPairUsesReportedForeground(t *testing.T) {
	t.Setenv("COLORFGBG", "")
	SetSystemTerminalColors(tui.TerminalColors{})
	SetSystemTerminalColorScheme(nil)
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		SetSystemTerminalColorScheme(nil)
	})
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	background := tui.RgbColor{R: 117, G: 117, B: 117}
	foreground := tui.RgbColor{}
	ui := &fakeThemeUI{terminalColors: tui.TerminalColors{
		Background: &background, Foreground: &foreground,
	}}
	setting := "light/dark"
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI: ui, InitialThemeSetting: &setting,
		Env: func(string) string { return "" },
	})
	defer controller.Dispose()
	controller.ApplyFromSettings()
	controller.ProbeSystemColors()
	if got := controller.ActiveThemeName(); got != "light" {
		t.Fatalf("theme = %q, want light for the reported foreground/background", got)
	}
	if got := controller.GetTerminalTheme(); got != TerminalThemeLight {
		t.Fatalf("terminal appearance = %q, want light", got)
	}
}

// pi v1.4.0 theme-controller.ts applyTerminalColorSchemeChange only reapplies
// when getTerminalTheme changes: a known background outranks a scheme report.
// In particular, do not flash the dark half of a pair while querying colors
// after tmux reports dark but the terminal's reported background is light.
func TestThemeControllerSchemeDoesNotOverrideKnownColors(t *testing.T) {
	t.Setenv("COLORFGBG", "")
	SetSystemTerminalColors(tui.TerminalColors{})
	SetSystemTerminalColorScheme(nil)
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		SetSystemTerminalColorScheme(nil)
	})
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	background := tui.RgbColor{R: 250, G: 250, B: 250}
	ui := &fakeThemeUI{terminalColors: tui.TerminalColors{Background: &background}}
	setting := "light/dark"
	var applied []string
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI: ui, InitialThemeSetting: &setting,
		Env:       func(string) string { return "" },
		OnChanged: func() { applied = append(applied, CurrentThemeName()) },
	})
	defer controller.Dispose()
	controller.ApplyFromSettings()
	controller.ProbeSystemColors()
	applied = nil
	ui.emit(TerminalThemeDark)
	for _, name := range applied {
		if name != "light" {
			t.Fatalf("scheme report applied %q despite the known light background; changes = %v", name, applied)
		}
	}
	if got := controller.GetTerminalTheme(); got != TerminalThemeLight {
		t.Fatalf("terminal appearance = %q, want light", got)
	}
}

// pi v1.4.0 theme-controller.ts applyTerminalColors invalidates and requests a
// frame even for a fixed named theme: tokens set to terminal-default colors
// can change without selecting a different theme.
func TestThemeControllerNamedThemeRepaintsReportedColors(t *testing.T) {
	SetSystemTerminalColors(tui.TerminalColors{})
	SetSystemTerminalColorScheme(nil)
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		SetSystemTerminalColorScheme(nil)
	})
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	foreground := tui.RgbColor{R: 220, G: 220, B: 220}
	ui := &fakeThemeUI{terminalColors: tui.TerminalColors{Foreground: &foreground}}
	setting := "dark"
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI: ui, InitialThemeSetting: &setting,
		Env: func(string) string { return "" },
	})
	defer controller.Dispose()
	controller.ApplyFromSettings()
	invalidates, renders := ui.invalidates, ui.renders
	controller.ProbeSystemColors()
	if ui.invalidates <= invalidates || ui.renders <= renders {
		t.Fatalf("changed colors did not repaint the named theme: invalidates %d to %d, renders %d to %d",
			invalidates, ui.invalidates, renders, ui.renders)
	}
	if got := controller.ActiveThemeName(); got != "dark" {
		t.Fatalf("color report changed the explicit theme to %q", got)
	}
}

// pi v1.4.0 theme-controller.ts applyTerminalColors compares merged RGB values,
// not object identities. Identical reports and timeouts must not rebuild the
// transcript, even when a terminal/tmux sends repeated appearance reports.
func TestThemeControllerUnchangedColorsDoNotInvalidate(t *testing.T) {
	t.Setenv("COLORFGBG", "")
	SetSystemTerminalColors(tui.TerminalColors{})
	SetSystemTerminalColorScheme(nil)
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		SetSystemTerminalColorScheme(nil)
	})
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	background := tui.RgbColor{R: 24, G: 28, B: 34}
	ui := &fakeThemeUI{terminalColors: tui.TerminalColors{Background: &background}}
	setting := SystemThemeName
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI: ui, InitialThemeSetting: &setting,
		Env: func(string) string { return "" },
	})
	defer controller.Dispose()
	controller.ApplyFromSettings()
	controller.ProbeSystemColors()
	invalidates := ui.invalidates
	queries := ui.systemProbes

	// An equal color is a distinct object, then a timeout reports no colors.
	copyOfBackground := background
	ui.terminalColors = tui.TerminalColors{Background: &copyOfBackground}
	ui.emit(TerminalThemeDark)
	ui.terminalColors = tui.TerminalColors{}
	ui.emit(TerminalThemeDark)
	if ui.systemProbes != queries+2 {
		t.Fatalf("color queries = %d, want %d", ui.systemProbes, queries+2)
	}
	if ui.invalidates != invalidates {
		t.Fatalf("unchanged colors invalidated %d times", ui.invalidates-invalidates)
	}
}
