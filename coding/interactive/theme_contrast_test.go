package interactive

import (
	"math"
	"testing"

	"github.com/dat267/pier/tui"
)

// D377: pier's built-in palettes (D154, D219) must stay readable. Upstream
// derives its palette from okhsl tokens and does not test contrast; the port's
// hand-tuned hex values drifted below WCAG AA for text drawn inside the signal
// panels, which is what made light and dark feel washed out. These floors are
// measured, not stylistic: normal text 4.5:1, dim/decorative text 3:1, and a
// panel fill visibly distinct from the page (1.2:1).
//
// The system theme is generated from the terminal's reported palette, so this
// gate covers the two documents in themes/; the generated theme's own values are
// pinned by testdata/systemtheme_golden.json.

const (
	minTextContrast = 4.5
	minDimContrast  = 3.0
	minFillOverPage = 1.2
)

func relativeLuminance(color tui.RgbColor) float64 {
	channel := func(value int) float64 {
		c := float64(value) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(color.R) + 0.7152*channel(color.G) + 0.0722*channel(color.B)
}

func contrastRatio(a, b tui.RgbColor) float64 {
	first, second := relativeLuminance(a), relativeLuminance(b)
	if first < second {
		first, second = second, first
	}
	return (first + 0.05) / (second + 0.05)
}

func builtinColor(t *testing.T, name, token string) (tui.RgbColor, bool) {
	t.Helper()
	palette := getBuiltinThemes()[name]
	colors := ResolveThemeColors(palette.Colors, palette.Vars)
	value, ok := colors[token]
	if !ok || value.IsIndex || value.Value == "" {
		return tui.RgbColor{}, false
	}
	color, err := tui.ParseColor(value.Value)
	if err != nil {
		t.Fatalf("%s.%s: parse %q: %v", name, token, value.Value, err)
	}
	return tui.ColorToRgb(color), true
}

func builtinPageColor(t *testing.T, name string) tui.RgbColor {
	t.Helper()
	export := getBuiltinThemes()[name].Export
	if export == nil || export.PageBg == nil || export.PageBg.IsIndex || export.PageBg.Value == "" {
		t.Fatalf("%s: theme document has no export.pageBg", name)
	}
	color, err := tui.ParseColor(export.PageBg.Value)
	if err != nil {
		t.Fatalf("%s: parse pageBg %q: %v", name, export.PageBg.Value, err)
	}
	return tui.ColorToRgb(color)
}

func TestBuiltinThemesMeetContrastTargets(t *testing.T) {
	fills := []string{"toolPendingBg", "toolSuccessBg", "toolErrorBg", "userMessageBg", "customMessageBg"}
	panelText := []string{
		"toolOutput", "muted", "thinkingText", "toolDiffContext", "mdQuote",
		"accent", "success", "error", "warning", "toolDiffAdded", "toolDiffRemoved",
		"customMessageLabel", "mdCode", "mdHeading", "mdLink", "mdListBullet",
	}
	pageText := append(append([]string{}, panelText...),
		"bashMode", "syntaxComment", "syntaxKeyword", "syntaxFunction", "syntaxVariable",
		"syntaxString", "syntaxNumber", "syntaxType", "syntaxOperator", "syntaxPunctuation",
	)
	dimText := []string{"dim", "mdLinkUrl", "thinkingOff", "thinkingMinimal"}

	for _, name := range []string{"dark", "light"} {
		page := builtinPageColor(t, name)
		for _, fillToken := range fills {
			fill, ok := builtinColor(t, name, fillToken)
			if !ok {
				t.Fatalf("%s.%s: missing fill", name, fillToken)
			}
			if ratio := contrastRatio(fill, page); ratio < minFillOverPage {
				t.Errorf("%s.%s is indistinguishable from the page: %.2f < %.2f", name, fillToken, ratio, minFillOverPage)
			}
			for _, token := range panelText {
				color, ok := builtinColor(t, name, token)
				if !ok {
					continue
				}
				if ratio := contrastRatio(color, fill); ratio < minTextContrast {
					t.Errorf("%s.%s on %s: %.2f < %.2f", name, token, fillToken, ratio, minTextContrast)
				}
			}
		}
		for _, token := range pageText {
			color, ok := builtinColor(t, name, token)
			if !ok {
				continue
			}
			if ratio := contrastRatio(color, page); ratio < minTextContrast {
				t.Errorf("%s.%s on the page: %.2f < %.2f", name, token, ratio, minTextContrast)
			}
		}
		for _, token := range dimText {
			color, ok := builtinColor(t, name, token)
			if !ok {
				continue
			}
			if ratio := contrastRatio(color, page); ratio < minDimContrast {
				t.Errorf("%s.%s on the page: %.2f < %.2f", name, token, ratio, minDimContrast)
			}
			for _, fillToken := range fills {
				fill, _ := builtinColor(t, name, fillToken)
				if ratio := contrastRatio(color, fill); ratio < minDimContrast {
					t.Errorf("%s.%s on %s: %.2f < %.2f", name, token, fillToken, ratio, minDimContrast)
				}
			}
		}
	}
}
