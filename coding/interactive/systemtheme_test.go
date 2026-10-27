package interactive

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// systemThemeGolden is the output of driving upstream
// modes/interactive/theme/system-theme.ts (bf8e4b953) with Node for the
// system-theme.test.ts terminals plus the grayscale and no-color cases.
type systemThemeGoldenCase struct {
	Colors     map[string]any `json:"colors"`
	Dim        []string       `json:"dim"`
	Appearance string         `json:"appearance"`
}

func loadSystemThemeGolden(t *testing.T) map[string]systemThemeGoldenCase {
	t.Helper()
	data, err := os.ReadFile("testdata/systemtheme_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	golden := map[string]systemThemeGoldenCase{}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	return golden
}

func systemTestHex(t *testing.T, hex string) tui.RgbColor {
	t.Helper()
	digits := strings.TrimPrefix(hex, "#")
	value, err := strconv.ParseUint(digits, 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	return tui.RgbColor{R: int(value >> 16 & 0xff), G: int(value >> 8 & 0xff), B: int(value & 0xff)}
}

var systemTestPalette = []string{
	"#21222c", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#f8f8f2",
	"#6272a4", "#ff6e6e", "#69ff94", "#ffffa5", "#d6acff", "#ff92df", "#a4ffff", "#ffffff",
}

func systemTestInput(t *testing.T, name string) SystemThemeInput {
	t.Helper()
	palette := func() []tui.RgbColor {
		out := make([]tui.RgbColor, 0, 16)
		for _, hex := range systemTestPalette {
			out = append(out, systemTestHex(t, hex))
		}
		return out
	}
	switch name {
	case "dracula":
		background := systemTestHex(t, "#282a36")
		foreground := systemTestHex(t, "#f8f8f2")
		return SystemThemeInput{Background: &background, Foreground: &foreground, Palette: palette()}
	case "draculaGray":
		input := systemTestInput(t, "dracula")
		input.Saturation = new(float64(0))
		return input
	case "solarizedLight":
		background := systemTestHex(t, "#fdf6e3")
		foreground := systemTestHex(t, "#657b83")
		return SystemThemeInput{Background: &background, Foreground: &foreground}
	case "backgroundOnly":
		background := systemTestHex(t, "#1e1e1e")
		return SystemThemeInput{Background: &background}
	case "midGray":
		background := systemTestHex(t, "#808080")
		foreground := systemTestHex(t, "#ffffff")
		return SystemThemeInput{Background: &background, Foreground: &foreground}
	case "noColorsLight":
		return SystemThemeInput{AppearanceHint: "light"}
	case "noColorsGray":
		return SystemThemeInput{Saturation: new(float64(0))}
	}
	t.Fatalf("unknown golden case %q", name)
	return SystemThemeInput{}
}

// TestSystemThemeAccentFollowsTerminalAppearance preserves the upstream
// system-theme.ts light/dark decision while giving Pier a distinct accent.
func TestSystemThemeAccentFollowsTerminalAppearance(t *testing.T) {
	t.Cleanup(func() {
		SetSystemTerminalColors(tui.TerminalColors{})
		SetSystemTerminalColorScheme(nil)
	})
	cases := []struct {
		name       string
		background tui.RgbColor
		appearance string
	}{
		{"dark terminal", tui.RgbColor{R: 24, G: 28, B: 34}, "dark"},
		{"light terminal", tui.RgbColor{R: 246, G: 244, B: 238}, "light"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SetSystemTerminalColors(tui.TerminalColors{Background: &tc.background})
			theme := buildSystemTheme(ColorModeTruecolor)
			if got := theme.Appearance(); got != tc.appearance {
				t.Fatalf("system appearance = %q, want %q", got, tc.appearance)
			}
			colors := theme.Colors()
			accentColor := colors["accent"]
			accent := tui.ColorToOklch(accentColor)
			distance := math.Abs(accent.H - 68)
			if distance > 180 {
				distance = 360 - distance
			}
			if distance > 5 {
				t.Errorf("system accent hue = %.1f°, want Pier amber near 68°", accent.H)
			}
			if got := WcagContrast(tui.ColorToRgb(accentColor), tc.background); got < systemTextMinimumContrast {
				t.Errorf("system accent contrast = %.2f, want at least %.1f", got, systemTextMinimumContrast)
			}
			for _, surface := range []string{"selectedBg", "toolPendingBg", "toolSuccessBg", "toolErrorBg"} {
				if got := WcagContrast(tui.ColorToRgb(accentColor), tui.ColorToRgb(colors[surface])); got < systemTextMinimumContrast {
					t.Errorf("system accent contrast against %s = %.2f, want at least %.1f", surface, got, systemTextMinimumContrast)
				}
			}
		})
	}
	SetSystemTerminalColors(tui.TerminalColors{})
	theme := buildSystemTheme(ColorModeTruecolor)
	accent := theme.Colors()["accent"]
	if accent.Kind != tui.ColorKindIndexed || accent.Index != 3 {
		t.Errorf("unreported-background accent = %+v, want terminal ANSI yellow index 3", accent)
	}
}

// TestSystemThemeAgainstUpstreamGolden reproduces the upstream generator
// byte for byte across the test terminals.
func TestSystemThemeAgainstUpstreamGolden(t *testing.T) {
	golden := loadSystemThemeGolden(t)
	for name, want := range golden {
		got := GenerateSystemThemeColors(systemTestInput(t, name))
		if got.Appearance != want.Appearance {
			t.Errorf("%s appearance = %q (want %q)", name, got.Appearance, want.Appearance)
		}
		for token, wantValue := range want.Colors {
			if fmt.Sprint(got.Colors[token]) != fmt.Sprint(wantValue) {
				t.Errorf("%s %s = %v (want %v)", name, token, got.Colors[token], wantValue)
			}
		}
		if strings.Join(got.Dim, ",") != strings.Join(want.Dim, ",") {
			t.Errorf("%s dim = %v (want %v)", name, got.Dim, want.Dim)
		}
	}
}

func systemTestResolved(t *testing.T, input SystemThemeInput, token string) tui.RgbColor {
	t.Helper()
	value := GenerateSystemThemeColors(input).Colors[token]
	if value == "" {
		if systemIsPanel(token) {
			return *input.Background
		}
		return *input.Foreground
	}
	hex, ok := value.(string)
	if !ok {
		t.Fatalf("%s resolved to %T, not a hex string", token, value)
	}
	return systemTestHex(t, hex)
}

// TestSystemThemeTextReadable is upstream's WCAG body-text assertion.
func TestSystemThemeTextReadable(t *testing.T) {
	for _, name := range []string{"dracula", "solarizedLight", "backgroundOnly", "midGray"} {
		input := systemTestInput(t, name)
		text := systemTestResolved(t, input, "text")
		selected := systemTestResolved(t, input, "selectedBg")
		for _, surface := range []tui.RgbColor{*input.Background, selected} {
			if contrast := WcagContrast(text, surface); contrast < 4.5 {
				t.Errorf("%s text contrast = %v", name, contrast)
			}
		}
		if contrast := WcagContrast(systemTestResolved(t, input, "toolTitle"), systemTestResolved(t, input, "toolErrorBg")); contrast < 4.5 {
			t.Errorf("%s toolTitle contrast = %v", name, contrast)
		}
	}
}

// TestSystemThemeRoleOrdering is upstream's contrast-ordering and panel
// proximity assertion.
func TestSystemThemeRoleOrdering(t *testing.T) {
	panels := []string{"userMessageBg", "toolPendingBg", "toolSuccessBg", "toolErrorBg", "selectedBg"}
	for _, name := range []string{"dracula", "solarizedLight", "backgroundOnly", "midGray"} {
		input := systemTestInput(t, name)
		background := tui.RgbToOklch(*input.Background).L
		offset := func(token string) float64 { return tui.RgbToOklch(systemTestResolved(t, input, token)).L - background }
		if name != "midGray" {
			if math.Abs(offset("text")) <= math.Abs(offset("muted")) || math.Abs(offset("muted")) <= math.Abs(offset("dim")) {
				t.Errorf("%s ordering text=%v muted=%v dim=%v", name, offset("text"), offset("muted"), offset("dim"))
			}
		}
		lighter := GenerateSystemThemeColors(input).Appearance == "dark"
		for _, panel := range panels {
			if contrast := WcagContrast(systemTestResolved(t, input, panel), *input.Background); contrast >= 2 {
				t.Errorf("%s %s contrast = %v", name, panel, contrast)
			}
			if (offset(panel) > 0) != lighter {
				t.Errorf("%s %s offset = %v, lighter = %v", name, panel, offset(panel), lighter)
			}
		}
	}
}

// TestSystemThemeUsesTerminalColors is upstream's foreground-hue assertion.
func TestSystemThemeUsesTerminalColors(t *testing.T) {
	dracula := systemTestInput(t, "dracula")
	if GenerateSystemThemeColors(dracula).Colors["text"] != "" {
		t.Error("dracula text did not use the terminal foreground")
	}
	if GenerateSystemThemeColors(systemTestInput(t, "solarizedLight")).Colors["text"] == "" {
		t.Error("solarizedLight text used an unreadable foreground")
	}
	hue := func(color tui.RgbColor) float64 { return tui.RgbToOklch(color).H }
	got := hue(systemTestResolved(t, dracula, "error"))
	want := hue(dracula.Palette[1])
	if delta := math.Abs(got - want); delta > 8 {
		t.Errorf("error hue = %v (want %v +/- 8)", got, want)
	}
}

// TestSystemThemeGrayscale is upstream's zero-saturation assertion.
func TestSystemThemeGrayscale(t *testing.T) {
	input := systemTestInput(t, "dracula")
	input.Saturation = new(float64(0))
	value := GenerateSystemThemeColors(input).Colors["error"]
	hex, ok := value.(string)
	if !ok {
		t.Fatalf("error = %T", value)
	}
	if chroma := tui.RgbToOklch(systemTestHex(t, hex)).C; chroma >= 0.005 {
		t.Errorf("grayscale error chroma = %v", chroma)
	}
}

// TestSystemThemeIndexedFallback is upstream's no-color-tier assertion.
func TestSystemThemeIndexedFallback(t *testing.T) {
	got := GenerateSystemThemeColors(SystemThemeInput{AppearanceHint: "light"})
	if got.Appearance != "light" {
		t.Errorf("appearance = %q", got.Appearance)
	}
	for token, want := range map[string]any{"error": 1, "text": "", "userMessageBg": ""} {
		if fmt.Sprint(got.Colors[token]) != fmt.Sprint(want) {
			t.Errorf("%s = %v (want %v)", token, got.Colors[token], want)
		}
	}
	if !systemHasString(got.Dim, "muted") {
		t.Errorf("dim = %v", got.Dim)
	}
	if value := GenerateSystemThemeColors(SystemThemeInput{Saturation: new(float64(0))}).Colors["error"]; value != "" {
		t.Errorf("zero saturation error = %v (want \"\")", value)
	}
}

// TestSystemThemeNoColorTierKeepsSignalPanels pins D193: when the terminal
// reports no colors, the port keeps its four signal fills so a running, failed
// and successful tool call stay distinguishable and a user message still reads
// as a panel. Upstream's indexedColors leaves every panel transparent
// (system-theme.ts), which filledBackgroundColors deliberately diverges from.
func TestSystemThemeNoColorTierKeepsSignalPanels(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	SetSystemTerminalColors(tui.TerminalColors{})
	for _, tc := range []struct{ name, colorfgbg, appearance string }{
		{name: "dark", colorfgbg: "", appearance: "dark"},
		{name: "light", colorfgbg: "0;15", appearance: "light"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COLORFGBG", tc.colorfgbg)
			theme := buildSystemTheme(ColorModeTruecolor)
			want := pierSignalPanelColors(tc.appearance)
			for _, token := range filledBackgroundColors {
				value, ok := want[token]
				if !ok {
					t.Fatalf("%s is not a port signal panel", token)
				}
				wantAnsi, _, _, err := colorValueAnsi(ColorValue{Value: value}, ColorModeTruecolor, true)
				if err != nil {
					t.Fatal(err)
				}
				if got := theme.GetBgAnsi(token); got != wantAnsi {
					t.Errorf("%s ansi = %q, want %q", token, got, wantAnsi)
				}
			}
		})
	}
}

// TestSystemThemePastelPalettesStayPastel is upstream's "keeps pastel palette
// colors pastel at other lightnesses" case (409e808f5, #10255): a palette
// color never gains OKLCH chroma when it moves to another lightness.
func TestSystemThemePastelPalettesStayPastel(t *testing.T) {
	bg := systemTestHex(t, "#303446")
	fg := systemTestHex(t, "#c6d0f5")
	hexes := []string{
		"#51576d", "#e78284", "#a6d189", "#e5c890", "#8caaee", "#f4b8e4", "#81c8be", "#b5bfe2",
		"#626880", "#e67172", "#8ec772", "#d9ba73", "#7b9ef0", "#f2a4db", "#5abfb5", "#a5adce",
	}
	palette := make([]tui.RgbColor, 0, 16)
	for _, hex := range hexes {
		palette = append(palette, systemTestHex(t, hex))
	}
	input := SystemThemeInput{Background: &bg, Foreground: &fg, Palette: palette}
	chroma := func(color tui.RgbColor) float64 { return tui.RgbToOklch(color).C }
	pink := palette[5]
	accent := systemTestResolved(t, input, "accent")
	// The accent is darker than the pink, but must not gain chroma (it was 2x
	// before the cap).
	if l := tui.RgbToOklch(accent).L; !(l < tui.RgbToOklch(pink).L-0.05) {
		t.Fatalf("accent lightness %v not below pink-0.05", l)
	}
	if c := chroma(accent); c > chroma(pink)*1.03 {
		t.Fatalf("accent chroma %v exceeds pink's %v", c, chroma(pink))
	}
	for _, panel := range []string{"userMessageBg", "customMessageBg"} {
		if c := chroma(systemTestResolved(t, input, panel)); c > 0.1 {
			t.Errorf("%s chroma = %v, want <= 0.1", panel, c)
		}
	}
}

// TestSystemThemeFaintTokens is upstream's "renders faint tokens with SGR 2
// and closes it" case: the no-color tier renders neutral tokens below body
// text faint.
func TestSystemThemeFaintTokens(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	SetSystemTerminalColors(tui.TerminalColors{})
	theme := buildSystemTheme(ColorModeTruecolor)
	if got := theme.GetFgAnsi("muted"); got != "\x1b[39m\x1b[2m" {
		t.Errorf("getFgAnsi(muted) = %q, want %q", got, "\x1b[39m\x1b[2m")
	}
	if got := theme.Fg("muted", "x"); got != "\x1b[39m\x1b[2mx\x1b[22;39m" {
		t.Errorf("fg(muted) = %q, want %q", got, "\x1b[39m\x1b[2mx\x1b[22;39m")
	}
	if got := theme.GetFgAnsi("error"); strings.Contains(got, "\x1b[2m") {
		t.Errorf("error should not be faint: %q", got)
	}
}

// TestSystemThemeWiring pins that the generator is reachable through the theme
// registry: "system" is listed first and its ANSI surface is built from the
// terminal colors.
func TestSystemThemeWiring(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	if names := AvailableThemes(); len(names) == 0 || names[0] != SystemThemeName {
		t.Fatalf("available themes = %v", names)
	}
	background := systemTestHex(t, "#282a36")
	foreground := systemTestHex(t, "#f8f8f2")
	SetSystemTerminalColors(tui.TerminalColors{Background: &background, Foreground: &foreground})
	theme := GetThemeByName(SystemThemeName)
	if theme == nil {
		t.Fatal("system theme did not load")
	}
	if got := theme.GetFgAnsi("text"); got != "\x1b[39m" {
		t.Fatalf("text ansi = %q", got)
	}
	if got := theme.GetFgAnsi("error"); !strings.HasPrefix(got, "\x1b[38;") {
		t.Fatalf("error ansi = %q", got)
	}
}
