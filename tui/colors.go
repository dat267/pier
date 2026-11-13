package tui

import (
	"fmt"
	"math"
	"reflect"
	"runtime"
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
			return Color{}, fmt.Errorf("%s must be between 0 and 255: %s", channel.name, jsNumberString(channel.value))
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
		return Color{}, fmt.Errorf("l must be between 0 and 1: %s", jsNumberString(l))
	}
	if c < 0 {
		return Color{}, fmt.Errorf("c must not be negative: %s", jsNumberString(c))
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
		return Color{}, fmt.Errorf("s must be between 0 and 1: %s", jsNumberString(s))
	}
	if l < 0 || l > 1 {
		return Color{}, fmt.Errorf("l must be between 0 and 1: %s", jsNumberString(l))
	}
	rgb := OkhslToRgb(h, s, l)
	return NewRgbColor(float64(rgb.R), float64(rgb.G), float64(rgb.B))
}

// ParseColor parses a theme color value: an ANSI palette index, #rgb/#rrggbb,
// oklch(...) or okhsl(...) (upstream parseColor).
func ParseColor(value any) (Color, error) {
	text, ok := value.(string)
	if !ok {
		number, numeric := numericColorValue(value)
		if !numeric {
			return Color{}, fmt.Errorf("Invalid color value: %v", value)
		}
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < 0 || number > 255 {
			return Color{}, fmt.Errorf("ANSI color index must be an integer from 0 to 255: %s", jsNumberString(number))
		}
		return IndexedColor(int(number))
	}
	if match := themeHexColorPattern.FindStringSubmatch(text); match != nil {
		digits := match[1]
		if len(digits) == 3 {
			digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
		}
		r, _ := strconv.ParseInt(digits[0:2], 16, 32)
		g, _ := strconv.ParseInt(digits[2:4], 16, 32)
		b, _ := strconv.ParseInt(digits[4:6], 16, 32)
		return NewRgbColor(float64(r), float64(g), float64(b))
	}
	if match := oklchColorPattern.FindStringSubmatch(text); match != nil {
		lightness := colorParseFloat(match[1])
		if match[2] != "" {
			lightness /= 100
		}
		return NewOklchColor(lightness, colorParseFloat(match[3]), colorParseFloat(match[4]))
	}
	if match := okhslColorPattern.FindStringSubmatch(text); match != nil {
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
	return Color{}, fmt.Errorf("Invalid color value: %s", text)
}

func numericColorValue(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	number := reflect.ValueOf(value)
	switch number.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(number.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(number.Uint()), true
	case reflect.Float32, reflect.Float64:
		return number.Float(), true
	default:
		return 0, false
	}
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

func colorDistance(r1, g1, b1, r2, g2, b2 float64) float64 {
	dr := r1 - r2
	dg := g1 - g2
	db := b1 - b2
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

// colorToRgbChannels converts any concrete color to sRGB channels the way
// upstream's colorToRgb does (D467): an OKLCH color is gamut-mapped and rounded
// by linearSrgbToRgb, while an RGB color keeps whatever channels it stores,
// which a mix can leave fractional. Mixing, the OKLCH/OKHSL conversions, the
// hex formatter and the ANSI downgrade all read this form, so the fractional
// ones survive until they are written.
func colorToRgbChannels(color Color) [3]float64 {
	switch color.Kind {
	case ColorKindIndexed:
		rgb := indexedToRgb(color.Index)
		return [3]float64{float64(rgb.R), float64(rgb.G), float64(rgb.B)}
	case ColorKindRGB:
		return [3]float64{color.R, color.G, color.B}
	default:
		rgb := OklchToRgb(color.L, color.C, color.H)
		return [3]float64{float64(rgb.R), float64(rgb.G), float64(rgb.B)}
	}
}

// ColorToRgb converts any concrete color to sRGB channels.
func ColorToRgb(color Color) RgbColor {
	channels := colorToRgbChannels(color)
	return RgbColor{R: int(math.Round(channels[0])), G: int(math.Round(channels[1])), B: int(math.Round(channels[2]))}
}

// ColorToOklch converts any concrete color to OKLCH channels.
func ColorToOklch(color Color) OklchChannels {
	if color.Kind == ColorKindOklch {
		return OklchChannels{L: color.L, C: color.C, H: color.H}
	}
	channels := colorToRgbChannels(color)
	lab := rgbToOklabChannels(channels[0], channels[1], channels[2])
	return OklchChannels{
		L: lab[0],
		C: math.Hypot(lab[1], lab[2]),
		H: math.Mod(math.Atan2(lab[2], lab[1])*180/math.Pi+360, 360),
	}
}

// ColorToOkhsl converts any concrete color to OKHSL channels.
func ColorToOkhsl(color Color) OkhslChannels {
	channels := colorToRgbChannels(color)
	return rgbToOkhslChannels(channels[0], channels[1], channels[2])
}

// ColorToHex formats a concrete color as #rrggbb.
func ColorToHex(color Color) string {
	channels := colorToRgbChannels(color)
	return fmt.Sprintf("#%02x%02x%02x",
		int(math.Round(channels[0])), int(math.Round(channels[1])), int(math.Round(channels[2])))
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
		return Color{}, fmt.Errorf("amount must be between 0 and 1: %s", jsNumberString(amount))
	}
	if space == ColorMixSrgb {
		a := colorToRgbChannels(first)
		b := colorToRgbChannels(second)
		return NewRgbColor(
			a[0]+(b[0]-a[0])*amount,
			a[1]+(b[1]-a[1])*amount,
			a[2]+(b[2]-a[2])*amount,
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

// rgbToAnsi256 downgrades unrounded channels to an xterm-256 index. Upstream
// compares the fractional channels, so a channel of exactly 47.5 stays closer to
// the cube's first level than to its second.
func rgbToAnsi256(channels [3]float64) int {
	rIndex := findClosestValue(cubeValues, channels[0])
	gIndex := findClosestValue(cubeValues, channels[1])
	bIndex := findClosestValue(cubeValues, channels[2])
	cubeColor := [3]float64{float64(cubeValues[rIndex]), float64(cubeValues[gIndex]), float64(cubeValues[bIndex])}
	cubeIndex := 16 + 36*rIndex + 6*gIndex + bIndex

	grays := grayValues()
	gray := math.Round(0.299*channels[0] + 0.587*channels[1] + 0.114*channels[2])
	grayOffset := findClosestValue(grays, gray)
	grayValue := float64(grays[grayOffset])
	spread := max(channels[0], max(channels[1], channels[2])) - min(channels[0], min(channels[1], channels[2]))
	if spread < 10 && colorDistance(channels[0], channels[1], channels[2], grayValue, grayValue, grayValue) <
		colorDistance(channels[0], channels[1], channels[2], cubeColor[0], cubeColor[1], cubeColor[2]) {
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
	channels := colorToRgbChannels(color)
	if mode == TerminalColorModeTruecolor {
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", layer,
			int(math.Round(channels[0])), int(math.Round(channels[1])), int(math.Round(channels[2])))
	}
	return fmt.Sprintf("\x1b[%d;5;%dm", layer, rgbToAnsi256(channels))
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

// TerminalDetectsTrueColor ports the truecolor branch of upstream's
// detectCapabilitiesFromEnvironment. Explicit terminal families win unless a
// multiplexer/proxy is detected first; otherwise COLORTERM and TERM provide the
// truecolor hint. The caller applies explicit environment and settings overrides.
func TerminalDetectsTrueColor(env func(string) string) bool {
	return terminalDetectsTrueColor(env, runtime.GOOS == "windows")
}

func terminalDetectsTrueColor(env func(string) string, windowsConsole bool) bool {
	if env == nil {
		return false
	}
	termProgram := strings.ToLower(env("TERM_PROGRAM"))
	terminalEmulator := strings.ToLower(env("TERMINAL_EMULATOR"))
	term := strings.ToLower(env("TERM"))
	colorTerm := strings.ToLower(env("COLORTERM"))
	hint := colorTerm == "truecolor" || colorTerm == "24bit" || strings.HasSuffix(term, "-direct")

	if env("TMUX") != "" || strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "screen") {
		return hint
	}
	if termProgram == "herdr" {
		return hint
	}
	if termProgram == "rex" {
		// Rex supports OSC 8 and enough of the Kitty graphics protocol for pi
		// (upstream 0ed1b0431/396ecd7b8, #11131).
		return true
	}
	if env("KITTY_WINDOW_ID") != "" || termProgram == "kitty" {
		return true
	}
	if termProgram == "ghostty" || strings.Contains(term, "ghostty") || env("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	if env("WEZTERM_PANE") != "" || termProgram == "wezterm" {
		return true
	}
	if termProgram == "warpterminal" || env("WARP_SESSION_ID") != "" || env("WARP_TERMINAL_SESSION_UUID") != "" {
		return true
	}
	if env("ITERM_SESSION_ID") != "" || termProgram == "iterm.app" {
		return true
	}
	if env("WT_SESSION") != "" {
		return true
	}
	// Alacritty does not set TERM_PROGRAM. It sets TERM=alacritty when its
	// terminfo entry is installed and ALACRITTY_WINDOW_ID on Unix and macOS
	// (pi v1.5.2 terminal-image.ts).
	if termProgram == "alacritty" || strings.HasPrefix(term, "alacritty") ||
		env("ALACRITTY_WINDOW_ID") != "" || termProgram == "vscode" || termProgram == "zed" {
		return true
	}
	if terminalEmulator == "jetbrains-jediterm" {
		return hint
	}
	if windowsConsole {
		return true
	}
	return hint
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
