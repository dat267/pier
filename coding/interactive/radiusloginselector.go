package interactive

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/dat267/pier/tui"
)

// Port of components/radius-login-selector.ts (pi v1.4.0, 4a080252).
// D156: the owner-loop animation scheduler replaces the upstream interval.
type radiusLoginMenuComponent struct {
	*ExtensionSelectorComponent
	radiusLabel, radiusText string
	animationStart          time.Time
	animating, disposed     bool
}

func newRadiusLoginMenu(title string, options []string, label, text string, onSelect func(string), onCancel func()) *radiusLoginMenuComponent {
	return &radiusLoginMenuComponent{
		ExtensionSelectorComponent: NewExtensionSelectorComponent(title, options, onSelect, onCancel),
		radiusLabel:                label, radiusText: text, animationStart: time.Now(),
	}
}

var radiusColors = []tui.Color{
	{Kind: tui.ColorKindRGB, R: 77, G: 154, B: 191},
	{Kind: tui.ColorKindRGB, R: 131, G: 204, B: 210},
	{Kind: tui.ColorKindRGB, R: 241, G: 190, B: 87},
	{Kind: tui.ColorKindRGB, R: 240, G: 144, B: 130},
}

func radiusShimmer(text string, elapsed time.Duration) string {
	mode := tuiColorMode(ActiveTheme().ColorMode())
	offset := elapsed.Seconds() * 10
	var result strings.Builder
	for index, char := range []rune(text) {
		position := math.Mod(math.Mod(float64(index)-offset, 16)+16, 16)
		band := int(position / 4)
		t := position/4 - float64(band)
		color, _ := tui.MixColors(radiusColors[band], radiusColors[(band+1)%len(radiusColors)], t*t*(3-2*t), tui.ColorMixSrgb)
		result.WriteString(tui.ForegroundAnsi(color, mode))
		result.WriteRune(char)
	}
	result.WriteString("\x1b[39m")
	return result.String()
}

func (c *radiusLoginMenuComponent) Render(width int) []string {
	lines := slices.Clone(c.ExtensionSelectorComponent.Render(width))
	c.animating = false
	if c.disposed {
		return lines
	}
	theme := ActiveTheme()
	selected := tui.NewText(theme.Fg("accent", "→ ")+theme.Fg("accent", c.radiusLabel), 1, 0, nil).Render(width)
	if len(selected) == 0 {
		return lines
	}
	for index, line := range lines {
		if line == selected[0] {
			c.animating = true
			animated := theme.Fg("accent", "→ ") + radiusShimmer(c.radiusText, time.Since(c.animationStart)) + strings.TrimPrefix(c.radiusLabel, c.radiusText)
			lines[index] = tui.NewText(animated, 1, 0, nil).Render(width)[0]
			break
		}
	}
	return lines
}

func (c *radiusLoginMenuComponent) AnimationFrame(time.Time) (bool, time.Duration) {
	if c.disposed || !c.animating {
		return false, 0
	}
	return true, 50 * time.Millisecond
}

func (c *radiusLoginMenuComponent) Dispose() {
	c.disposed, c.animating = true, false
	c.ExtensionSelectorComponent.Dispose()
}
