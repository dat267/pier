package interactive

import (
	"math"
	"time"

	"github.com/dat267/pier/tui"
)

// Port of components/provider-brand.ts (pi v1.5.2 b4afdddf): the animated name
// of a preferred partner provider, used by the auth selector's selected row.

// brandAnimationFrame is the repaint interval upstream schedules.
const brandAnimationFrame = 50 * time.Millisecond

// getProviderBrandStyle returns the animated style of a provider's name, or nil
// when the provider has none.
func getProviderBrandStyle(providerID string) func(text string, elapsed time.Duration) string {
	switch providerID {
	case "radius":
		return radiusShimmer
	case "openrouter":
		return openRouterGlow
	}
	return nil
}

// OpenRouter's brand colors per theme appearance: lime on dark backgrounds,
// violet on light ones, where lime would be unreadable. The glow breathes
// between base and the brighter peak.
var (
	openRouterGlowDark = [2]tui.Color{
		{Kind: tui.ColorKindRGB, R: 0xc6, G: 0xf2, B: 0x43},
		{Kind: tui.ColorKindRGB, R: 0xef, G: 0xff, B: 0xb0},
	}
	openRouterGlowLight = [2]tui.Color{
		{Kind: tui.ColorKindRGB, R: 0x5d, G: 0x32, B: 0xe1},
		{Kind: tui.ColorKindRGB, R: 0x8f, G: 0x6b, B: 0xff},
	}
)

const openRouterGlowPeriodMS = 2400

// openRouterGlow colors text in OpenRouter's brand color, slowly brightening and
// dimming.
func openRouterGlow(text string, elapsed time.Duration) string {
	palette := openRouterGlowDark
	if ActiveTheme().Appearance() != "dark" {
		palette = openRouterGlowLight
	}
	elapsedMS := float64(elapsed.Milliseconds())
	amount := (1 - math.Cos(2*math.Pi*elapsedMS/openRouterGlowPeriodMS)) / 2
	color, _ := tui.MixColors(palette[0], palette[1], amount, tui.ColorMixOklch)
	return tui.ForegroundAnsi(color, tuiColorMode(ActiveTheme().ColorMode())) + text + "\x1b[39m"
}

// brandText returns the selected row's provider name, animated for a preferred
// partner and accent-colored otherwise.
func brandText(providerID, name string, elapsed time.Duration) string {
	if style := getProviderBrandStyle(providerID); style != nil {
		return style(name, elapsed)
	}
	return ActiveTheme().Fg("accent", name)
}
