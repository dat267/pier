package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// Port of packages/coding-agent/test/theme-style.test.ts (upstream 567469096).

// loadStyledTheme loads a built-in theme, applies edit to its document, and
// builds it in truecolor mode from a temp file.
func loadStyledTheme(t *testing.T, base string, edit func(map[string]any)) *Theme {
	t.Helper()
	data, err := builtinThemeFS.ReadFile("themes/" + base + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(doc)
	}
	edited, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), base+".json")
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	theme, err := LoadThemeFromPath(path, ColorModeTruecolor)
	if err != nil {
		t.Fatal(err)
	}
	return theme
}

func themeColors(doc map[string]any) map[string]any {
	return doc["colors"].(map[string]any)
}

func mustPanic(t *testing.T, what string, run func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", what)
		}
	}()
	run()
}

func TestThemeStyleMatchesTextStyler(t *testing.T) {
	theme := loadStyledTheme(t, "dark", nil)
	got := theme.Style("Ready", ThemeStyle{
		Fg:             "success",
		Bg:             "toolSuccessBg",
		TextAttributes: tui.TextAttributes{Bold: true},
	})
	success := theme.Colors()["success"]
	toolSuccessBg := theme.Colors()["toolSuccessBg"]
	want := tui.StyleText("Ready", tui.TextStyle{
		Fg:             &success,
		Bg:             &toolSuccessBg,
		TextAttributes: tui.TextAttributes{Bold: true},
	}, tui.TerminalColorModeTruecolor)
	if got != want {
		t.Errorf("theme.style = %q, want %q", got, want)
	}
}

func TestThemeStyleRejectsUnknownAndWrongSlotTokens(t *testing.T) {
	theme := loadStyledTheme(t, "dark", nil)
	mustPanic(t, "unknown token", func() {
		theme.Style("x", ThemeStyle{Fg: "notAToken"})
	})
	mustPanic(t, "background token as foreground", func() {
		theme.Style("x", ThemeStyle{Fg: "userMessageBg"})
	})
}

func TestThemeStyleLoadsOklchValues(t *testing.T) {
	theme := loadStyledTheme(t, "dark", func(doc map[string]any) {
		themeColors(doc)["accent"] = "oklch(62% 0.1 200)"
	})
	want, err := tui.ParseColor("oklch(62% 0.1 200)")
	if err != nil {
		t.Fatal(err)
	}
	if got := theme.Colors()["accent"]; got != want {
		t.Errorf("accent = %+v, want %+v", got, want)
	}
}

func TestThemeStyleLoadsOkhslValuesIncludingVars(t *testing.T) {
	theme := loadStyledTheme(t, "dark", func(doc map[string]any) {
		vars, _ := doc["vars"].(map[string]any)
		if vars == nil {
			vars = map[string]any{}
		}
		vars["brand"] = "okhsl(250 60% 55%)"
		doc["vars"] = vars
		themeColors(doc)["accent"] = "brand"
		themeColors(doc)["error"] = "okhsl(20 90% 60%)"
	})
	brand, err := tui.NewOkhslColor(250, 0.6, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	if got := tui.ColorToHex(theme.Colors()["accent"]); got != tui.ColorToHex(brand) {
		t.Errorf("accent = %s, want %s", got, tui.ColorToHex(brand))
	}
	errColor, err := tui.NewOkhslColor(20, 0.9, 0.6)
	if err != nil {
		t.Fatal(err)
	}
	if got := tui.ColorToHex(theme.Colors()["error"]); got != tui.ColorToHex(errColor) {
		t.Errorf("error = %s, want %s", got, tui.ColorToHex(errColor))
	}
}

func TestThemeStyleDetectsAppearance(t *testing.T) {
	t.Cleanup(func() { SetSystemTerminalColors(tui.TerminalColors{}) })
	if got := loadStyledTheme(t, "dark", nil).Appearance(); got != "dark" {
		t.Errorf("dark appearance = %q", got)
	}
	if got := loadStyledTheme(t, "light", nil).Appearance(); got != "light" {
		t.Errorf("light appearance = %q", got)
	}
	for _, base := range []string{"dark", "light"} {
		theme := loadStyledTheme(t, base, func(doc map[string]any) { delete(doc, "appearance") })
		if got := theme.Appearance(); got != base {
			t.Errorf("%s without appearance = %q", base, got)
		}
	}
	declared := loadStyledTheme(t, "dark", func(doc map[string]any) { doc["appearance"] = "light" })
	if got := declared.Appearance(); got != "light" {
		t.Errorf("declared appearance = %q", got)
	}

	// Palette colors 0-15 follow the terminal palette, so such themes follow the
	// terminal background.
	paletteOnly := loadStyledTheme(t, "dark", func(doc map[string]any) {
		delete(doc, "appearance")
		for key := range themeColors(doc) {
			if strings.HasSuffix(key, "Bg") {
				themeColors(doc)[key] = 0
			} else {
				themeColors(doc)[key] = 7
			}
		}
	})
	if got := paletteOnly.Appearance(); got != "dark" {
		t.Errorf("palette-only appearance = %q", got)
	}
	SetSystemTerminalColors(tui.TerminalColors{Background: &tui.RgbColor{R: 250, G: 250, B: 250}})
	if got := paletteOnly.Appearance(); got != "light" {
		t.Errorf("palette-only appearance after light background = %q", got)
	}
}

func TestThemeStyleEmptyTokensUseTerminalDefaults(t *testing.T) {
	t.Cleanup(func() { SetSystemTerminalColors(tui.TerminalColors{}) })
	theme := loadStyledTheme(t, "dark", func(doc map[string]any) {
		themeColors(doc)["text"] = ""
		themeColors(doc)["userMessageBg"] = ""
	})
	if got := theme.Fg("text", "x"); got != "\x1b[39mx\x1b[39m" {
		t.Errorf("fg(text) = %q", got)
	}
	if got := theme.Bg("userMessageBg", "x"); got != "\x1b[49mx\x1b[49m" {
		t.Errorf("bg(userMessageBg) = %q", got)
	}
	if got := tui.ColorToHex(theme.Colors()["text"]); got != "#e5e5e7" {
		t.Errorf("default text = %s", got)
	}
	if got := tui.ColorToHex(theme.Colors()["userMessageBg"]); got != "#000000" {
		t.Errorf("default userMessageBg = %s", got)
	}
	SetSystemTerminalColors(tui.TerminalColors{
		Foreground: &tui.RgbColor{R: 200, G: 210, B: 220},
		Background: &tui.RgbColor{R: 10, G: 20, B: 30},
	})
	if got := tui.ColorToHex(theme.Colors()["text"]); got != "#c8d2dc" {
		t.Errorf("reported text = %s", got)
	}
	if got := tui.ColorToHex(theme.Colors()["userMessageBg"]); got != "#0a141e" {
		t.Errorf("reported userMessageBg = %s", got)
	}
}
