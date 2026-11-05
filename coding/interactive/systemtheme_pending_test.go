package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// Reference: pi f9a26365 modes/interactive/theme/theme-controller.ts marks the
// terminal colors pending in its constructor and theme.ts createSystemTheme
// passes `saturation: terminalColorsPending ? 0 : 1`. The system theme therefore
// starts grayscale (terminal-default tokens) and takes the reported palette
// after the first report. Without the pending state the first frames already
// use ANSI palette indices, so what the user sees at startup depends on whether
// the terminal's color reply beats the first paint.
func TestSystemThemeStartsGrayscaleUntilColorsAreReported(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		SetSystemTerminalColorScheme(nil)
		SetRegisteredThemes(nil)
		SetCustomThemesDir("")
	})

	controller := NewInteractiveThemeController(ThemeControllerOptions{Env: func(string) string { return "" }})
	defer controller.Dispose()
	if got := controller.ActiveThemeName(); got != SystemThemeName {
		t.Fatalf("default theme = %q, want system", got)
	}
	if styled := ActiveTheme().Fg("accent", "x"); strings.Contains(styled, "\x1b[38;") {
		t.Fatalf("system theme used a palette color before any report: %q", styled)
	}
	// The tool/message panel fills follow the detected appearance, so they must
	// not be chosen from the environment fallback: that produced deep-green
	// boxes on one launch and the reported variant on the next.
	if fill := ActiveTheme().Bg("toolSuccessBg", "x"); strings.Contains(fill, "\x1b[48;") {
		t.Fatalf("system theme filled a tool panel before any report: %q", fill)
	}

	// The first report ends the pending state, exactly like setTerminalColors.
	background := tui.RgbColor{R: 24, G: 28, B: 34}
	SetSystemTerminalColors(tui.TerminalColors{Background: &background})
	InitTheme(SystemThemeName, false)
	if styled := ActiveTheme().Fg("accent", "x"); !strings.Contains(styled, "\x1b[38;") {
		t.Fatalf("system theme kept the pending palette after a report: %q", styled)
	}
	if fill := ActiveTheme().Bg("toolSuccessBg", "x"); !strings.Contains(fill, "\x1b[48;") {
		t.Fatalf("system theme did not fill tool panels after a report: %q", fill)
	}
}
