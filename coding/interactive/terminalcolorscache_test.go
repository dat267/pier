package interactive

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

// TestTerminalColorsCacheRoundTrip pins the persisted terminal colors that seed
// the system theme before the OSC query replies (D221).
func TestTerminalColorsCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if LoadSystemTerminalColorsCache(dir) {
		t.Fatal("an empty agent dir must not load a cache")
	}
	// A cache with nothing reported is not written.
	saveTerminalColorsCache(dir, tui.TerminalColors{})
	if _, err := os.Stat(filepath.Join(dir, TerminalColorsCacheFileName)); err == nil {
		t.Fatal("empty colors must not be cached")
	}

	background := tui.RgbColor{R: 0xfa, G: 0xfa, B: 0xfa}
	foreground := tui.RgbColor{R: 0x20, G: 0x20, B: 0x20}
	palette := []tui.RgbColor{{R: 1, G: 2, B: 3}, {R: 4, G: 5, B: 6}}
	saveTerminalColorsCache(dir, tui.TerminalColors{
		Background: &background, Foreground: &foreground, Palette: palette,
	})

	// Clear the in-memory state so the load is observable.
	SetSystemTerminalColors(tui.TerminalColors{})
	if !LoadSystemTerminalColorsCache(dir) {
		t.Fatal("cache did not load")
	}
	colors := systemThemeTerminalColors()
	if colors == nil || colors.Background == nil || *colors.Background != background {
		t.Fatalf("background = %+v", colors)
	}
	if colors.Foreground == nil || *colors.Foreground != foreground {
		t.Fatalf("foreground = %+v", colors)
	}
	if len(colors.Palette) != len(palette) || colors.Palette[0] != palette[0] {
		t.Fatalf("palette = %+v", colors.Palette)
	}
}

// TestThemeControllerPersistsTerminalColors pins that a settled system-color
// query writes the cache off the owner loop, so the next launch's first paint
// already matches this terminal (D221).
func TestThemeControllerPersistsTerminalColors(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	agentDir := t.TempDir()

	ui := &fakeThemeUI{}
	background := tui.RgbColor{R: 0xfa, G: 0xfa, B: 0xfa}
	foreground := tui.RgbColor{R: 0x20, G: 0x20, B: 0x20}
	ui.terminalColors = tui.TerminalColors{Background: &background, Foreground: &foreground}

	applied := make(chan struct{}, 8)
	controller := NewInteractiveThemeController(ThemeControllerOptions{
		UI: ui, AgentDir: agentDir, TimeoutMS: 1,
		Env: func(string) string { return "" },
		Marshal: func(fn func()) {
			fn()
			select {
			case applied <- struct{}{}:
			default:
			}
		},
	})
	if result := controller.SetThemeName(SystemThemeName, false); !result.Success {
		t.Fatalf("system theme did not load: %+v", result)
	}
	controller.ProbeSystemColors()
	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal colors were not applied")
	}
	// The write runs before the marshal, so the file exists once applied fires.
	if _, err := os.Stat(filepath.Join(agentDir, TerminalColorsCacheFileName)); err != nil {
		t.Fatalf("terminal colors were not cached: %v", err)
	}
	if !LoadSystemTerminalColorsCache(agentDir) {
		t.Fatal("cached terminal colors did not load")
	}
	if colors := systemThemeTerminalColors(); colors == nil || colors.Background == nil || *colors.Background != background {
		t.Fatalf("cached background = %+v", colors)
	}
}
