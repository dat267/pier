package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Port of packages/tui/src/colors.ts (upstream 567469096): concrete color
// values (ANSI palette index, sRGB, OKLCH), the conversions that map any of
// them to sRGB, mixing, and the text stylers the interactive theme builds on.

// TerminalColorMode names the escape form a concrete color is written in.
type TerminalColorMode string

const (
	TerminalColorMode256       TerminalColorMode = "256color"
	TerminalColorModeTruecolor TerminalColorMode = "truecolor"
)

// ColorKind discriminates the concrete color variants.
type ColorKind uint8

const (
	ColorKindIndexed ColorKind = iota
	ColorKindRGB
	ColorKindOklch
)

// Color is a concrete color: an ANSI palette index, sRGB channels, or OKLCH.
// Every variant converts to sRGB, so color math never depends on the variant.
type Color struct {
	Kind  ColorKind
	Index int
	R     float64
	G     float64
	B     float64
	L     float64
	C     float64
	H     float64
}

func requireFinite(value float64, name string) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s must be finite", name)
	}
	return nil
}

// IndexedColor builds a palette-index color.
func IndexedColor(index int) (Color, error) {
	if index < 0 || index > 255 {
		return Color{}, fmt.Errorf("ANSI color index must be an integer from 0 to 255: %d", index)
	}
	return Color{Kind: ColorKindIndexed, Index: index}, nil
}

// NewRgbColor builds an sRGB color.
func NewRgbColor(r, g, b float64) (Color, error) {
	for _, channel := range []struct {
		name  string
		value float64
	}{{"r", r}, {"g", g}, {"b", b}} {
		if err := requireFinite(channel.value, channel.name); err != nil {
			return Color{}, err
		}
		if channel.value < 0 || channel.value > 255 {
			return Color{}, fmt.Errorf("%s must be between 0 and 255: %v", channel.name, channel.value)
		}
	}
	return Color{Kind: ColorKindRGB, R: r, G: g, B: b}, nil
}

// NewOklchColor builds an OKLCH color, normalizing the hue to 0-360.
func NewOklchColor(l, c, h float64) (Color, error) {
	for _, value := range []struct {
		name  string
		value float64
	}{{"l", l}, {"c", c}, {"h", h}} {
		if err := requireFinite(value.value, value.name); err != nil {
			return Color{}, err
		}
	}
	if l < 0 || l > 1 {
		return Color{}, fmt.Errorf("l must be between 0 and 1: %v", l)
	}
	if c < 0 {
		return Color{}, fmt.Errorf("c must not be negative: %v", c)
	}
	return Color{Kind: ColorKindOklch, L: l, C: c, H: math.Mod(math.Mod(h, 360)+360, 360)}, nil
}

// NewOkhslColor builds an sRGB color from OKHSL channels. Saturation is
// validated, then resolved against the sRGB gamut at the hue and lightness.
func NewOkhslColor(h, s, l float64) (Color, error) {
	if err := requireFinite(h, "h"); err != nil {
		return Color{}, err
	}
	if err := requireFinite(s, "s"); err != nil {
		return Color{}, err
	}
	if err := requireFinite(l, "l"); err != nil {
		return Color{}, err
	}
	if s < 0 || s > 1 {
		return Color{}, fmt.Errorf("s must be between 0 and 1: %v", s)
	}
	if l < 0 || l > 1 {
		return Color{}, fmt.Errorf("l must be between 0 and 1: %v", l)
	}
	rgb := OkhslToRgb(h, s, l)
	return NewRgbColor(float64(rgb.R), float64(rgb.G), float64(rgb.B))
}

// ParseColor parses a theme color value: #rgb/#rrggbb, oklch(...) or
// okhsl(...). Unlike upstream's parseColor it takes only a string; palette
// indices arrive as a separate value in the theme document.
func ParseColor(value string) (Color, error) {
	if match := themeHexColorPattern.FindStringSubmatch(value); match != nil {
		digits := match[1]
		if len(digits) == 3 {
			digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
		}
		r, _ := strconv.ParseInt(digits[0:2], 16, 32)
		g, _ := strconv.ParseInt(digits[2:4], 16, 32)
		b, _ := strconv.ParseInt(digits[4:6], 16, 32)
		return NewRgbColor(float64(r), float64(g), float64(b))
	}
	if match := oklchColorPattern.FindStringSubmatch(value); match != nil {
		lightness := colorParseFloat(match[1])
		if match[2] != "" {
			lightness /= 100
		}
		return NewOklchColor(lightness, colorParseFloat(match[3]), colorParseFloat(match[4]))
	}
	if match := okhslColorPattern.FindStringSubmatch(value); match != nil {
		saturation := colorParseFloat(match[2])
		if match[3] != "" {
			saturation /= 100
		}
		lightness := colorParseFloat(match[4])
		if match[5] != "" {
			lightness /= 100
		}
		return NewOkhslColor(colorParseFloat(match[1]), saturation, lightness)
	}
	return Color{}, fmt.Errorf("Invalid color value: %s", value)
}

var basicIndexedColors = [16]RgbColor{
	{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
	{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
	{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

var cubeValues = []int{0, 95, 135, 175, 215, 255}

func grayValues() []int {
	values := make([]int, 24)
	for index := range values {
		values[index] = 8 + index*10
	}
	return values
}

func colorDistance(r1, g1, b1, r2, g2, b2 int) float64 {
	dr := float64(r1 - r2)
	dg := float64(g1 - g2)
	db := float64(b1 - b2)
	return dr*dr*0.299 + dg*dg*0.587 + db*db*0.114
}

func indexedToRgb(index int) RgbColor {
	if index < 16 {
		return basicIndexedColors[index]
	}
	if index < 232 {
		cubeIndex := index - 16
		return RgbColor{
			R: cubeValues[cubeIndex/36],
			G: cubeValues[(cubeIndex%36)/6],
			B: cubeValues[cubeIndex%6],
		}
	}
	gray := 8 + (index-232)*10
	return RgbColor{R: gray, G: gray, B: gray}
}

// ColorToRgb converts any concrete color to sRGB channels.
func ColorToRgb(color Color) RgbColor {
	switch color.Kind {
	case ColorKindIndexed:
		return indexedToRgb(color.Index)
	case ColorKindRGB:
		return RgbColor{
			R: int(math.Round(color.R)),
			G: int(math.Round(color.G)),
			B: int(math.Round(color.B)),
		}
	default:
		return OklchToRgb(color.L, color.C, color.H)
	}
}

// ColorToOklch converts any concrete color to OKLCH channels.
func ColorToOklch(color Color) OklchChannels {
	if color.Kind == ColorKindOklch {
		return OklchChannels{L: color.L, C: color.C, H: color.H}
	}
	return RgbToOklch(ColorToRgb(color))
}

// ColorToOkhsl converts any concrete color to OKHSL channels.
func ColorToOkhsl(color Color) OkhslChannels {
	return RgbToOkhsl(ColorToRgb(color))
}

// ColorToHex formats a concrete color as #rrggbb.
func ColorToHex(color Color) string {
	rgb := ColorToRgb(color)
	return fmt.Sprintf("#%02x%02x%02x", rgb.R, rgb.G, rgb.B)
}

// ColorMixSpace selects the interpolation space for MixColors.
type ColorMixSpace string

const (
	ColorMixOklch ColorMixSpace = "oklch"
	ColorMixSrgb  ColorMixSpace = "srgb"
)

// MixColors interpolates two colors, by default in OKLCH.
func MixColors(first, second Color, amount float64, space ColorMixSpace) (Color, error) {
	if err := requireFinite(amount, "amount"); err != nil {
		return Color{}, err
	}
	if amount < 0 || amount > 1 {
		return Color{}, fmt.Errorf("amount must be between 0 and 1: %v", amount)
	}
	if space == ColorMixSrgb {
		a := ColorToRgb(first)
		b := ColorToRgb(second)
		return NewRgbColor(
			float64(a.R)+float64(b.R-a.R)*amount,
			float64(a.G)+float64(b.G-a.G)*amount,
			float64(a.B)+float64(b.B-a.B)*amount,
		)
	}
	a := ColorToOklch(first)
	b := ColorToOklch(second)
	firstHue := a.H
	if a.C < 1e-7 {
		firstHue = b.H
	}
	secondHue := b.H
	if b.C < 1e-7 {
		secondHue = firstHue
	}
	hueDelta := math.Mod(secondHue-firstHue+540, 360) - 180
	return NewOklchColor(a.L+(b.L-a.L)*amount, a.C+(b.C-a.C)*amount, firstHue+hueDelta*amount)
}

func findClosestValue(values []int, target float64) int {
	closestIndex := 0
	closestDistance := math.Inf(1)
	for index, value := range values {
		distance := math.Abs(target - float64(value))
		if distance < closestDistance {
			closestIndex = index
			closestDistance = distance
		}
	}
	return closestIndex
}

func rgbToAnsi256(color RgbColor) int {
	rIndex := findClosestValue(cubeValues, float64(color.R))
	gIndex := findClosestValue(cubeValues, float64(color.G))
	bIndex := findClosestValue(cubeValues, float64(color.B))
	cubeColor := RgbColor{R: cubeValues[rIndex], G: cubeValues[gIndex], B: cubeValues[bIndex]}
	cubeIndex := 16 + 36*rIndex + 6*gIndex + bIndex

	grays := grayValues()
	gray := int(math.Round(0.299*float64(color.R) + 0.587*float64(color.G) + 0.114*float64(color.B)))
	grayOffset := findClosestValue(grays, float64(gray))
	grayValue := grays[grayOffset]
	spread := max(color.R, max(color.G, color.B)) - min(color.R, min(color.G, color.B))
	if spread < 10 && colorDistance(color.R, color.G, color.B, grayValue, grayValue, grayValue) <
		colorDistance(color.R, color.G, color.B, cubeColor.R, cubeColor.G, cubeColor.B) {
		return 232 + grayOffset
	}
	return cubeIndex
}

func colorAnsi(color Color, mode TerminalColorMode, background bool) string {
	layer := 38
	if background {
		layer = 48
	}
	if color.Kind == ColorKindIndexed {
		return fmt.Sprintf("\x1b[%d;5;%dm", layer, color.Index)
	}
	rgb := ColorToRgb(color)
	if mode == TerminalColorModeTruecolor {
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", layer, rgb.R, rgb.G, rgb.B)
	}
	return fmt.Sprintf("\x1b[%d;5;%dm", layer, rgbToAnsi256(rgb))
}

// ForegroundAnsi returns the foreground escape sequence for a concrete color.
func ForegroundAnsi(color Color, mode TerminalColorMode) string {
	return colorAnsi(color, mode, false)
}

// BackgroundAnsi returns the background escape sequence for a concrete color.
func BackgroundAnsi(color Color, mode TerminalColorMode) string {
	return colorAnsi(color, mode, true)
}

// TextAttributes are the non-color text attributes.
type TextAttributes struct {
	Bold          bool
	Dim           bool
	Italic        bool
	Underline     bool
	Inverse       bool
	Strikethrough bool
}

// TextStyle combines a foreground and background color with text attributes.
type TextStyle struct {
	TextAttributes
	Fg *Color
	Bg *Color
}

// TerminalDetectsTrueColor reports the terminal's truecolor hint from the
// environment: COLORTERM "truecolor"/"24bit", or a TERM ending in "-direct"
// (upstream detectCapabilitiesFromEnvironment). The host injects the capability
// flag in the port (D69); this mirrors the environment rule.
func TerminalDetectsTrueColor(env func(string) string) bool {
	if env == nil {
		return false
	}
	switch strings.ToLower(env("COLORTERM")) {
	case "truecolor", "24bit":
		return true
	}
	return strings.HasSuffix(strings.ToLower(env("TERM")), "-direct")
}

// GetTerminalColorMode returns the escape mode matching the injected terminal
// capabilities (upstream getTerminalColorMode).
func GetTerminalColorMode() TerminalColorMode {
	if GetTerminalCapabilities().TrueColor {
		return TerminalColorModeTruecolor
	}
	return TerminalColorMode256
}

// StyleText styles text with concrete colors and attributes.
func StyleText(text string, options TextStyle, mode TerminalColorMode) string {
	fgAnsi := ""
	if options.Fg != nil {
		fgAnsi = ForegroundAnsi(*options.Fg, mode)
	}
	bgAnsi := ""
	if options.Bg != nil {
		bgAnsi = BackgroundAnsi(*options.Bg, mode)
	}
	return StyleTextWithAnsi(text, fgAnsi, bgAnsi, options.TextAttributes)
}

// StyleTextWithAnsi is StyleText with precomputed color sequences; the colors
// in options are ignored. Resets are prepended so they close in reverse order
// of the opening sequences.
func StyleTextWithAnsi(text string, fgAnsi string, bgAnsi string, options TextAttributes) string {
	var prefix, suffix string
	if fgAnsi != "" {
		prefix += fgAnsi
		suffix = "\x1b[39m"
	}
	if bgAnsi != "" {
		prefix += bgAnsi
		suffix = "\x1b[49m" + suffix
	}
	if options.Bold {
		prefix += "\x1b[1m"
	}
	if options.Dim {
		prefix += "\x1b[2m"
	}
	if options.Bold || options.Dim {
		suffix = "\x1b[22m" + suffix
	}
	if options.Italic {
		prefix += "\x1b[3m"
		suffix = "\x1b[23m" + suffix
	}
	if options.Underline {
		prefix += "\x1b[4m"
		suffix = "\x1b[24m" + suffix
	}
	if options.Inverse {
		prefix += "\x1b[7m"
		suffix = "\x1b[27m" + suffix
	}
	if options.Strikethrough {
		prefix += "\x1b[9m"
		suffix = "\x1b[29m" + suffix
	}
	return prefix + text + suffix
}
