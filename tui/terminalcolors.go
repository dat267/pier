package tui

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Port of src/terminal-colors.ts and the terminal color queries of src/tui.ts.

// RgbColor is an RGB color.
type RgbColor struct {
	R int
	G int
	B int
}

// TerminalColors are the colors the terminal reports for its current theme.
type TerminalColors struct {
	// Foreground is the default foreground (OSC 10), if reported.
	Foreground *RgbColor
	// Background is the default background (OSC 11), if reported.
	Background *RgbColor
	// Palette is ANSI colors 0-15; only set when all 16 were reported.
	Palette []RgbColor
}

// OscColorTarget names what an OSC color reply reports: the default foreground
// (OSC 10), the default background (OSC 11), or a palette index (OSC 4).
type OscColorTarget struct {
	Foreground bool
	Background bool
	Index      int
}

// OscColorResponse is a parsed OSC 10, 11, or 4 reply. HasRGB is false when the
// reply carried an unparseable color.
type OscColorResponse struct {
	Target OscColorTarget
	RGB    RgbColor
	HasRGB bool
}

// TerminalColorScheme is the terminal's light/dark preference.
type TerminalColorScheme = string

// Terminal color schemes.
const (
	TerminalColorSchemeDark  TerminalColorScheme = "dark"
	TerminalColorSchemeLight TerminalColorScheme = "light"
)

var (
	oscColorResponsePattern         = regexp.MustCompile(`(?is)^\x1b\](?:(1[01])|4;(\d{1,3}));([^\x07\x1b]*)(?:\x07|\x1b\\)$`)
	deviceAttributesResponsePattern = regexp.MustCompile(`^\x1b\[\?[\d;]*c$`)
	colorSchemeReportPattern        = regexp.MustCompile(`^(?:\x1b\[\?997;(1|2)n)+$`)
	oscHexChannelPattern            = regexp.MustCompile(`(?i)^[0-9a-f]+$`)
	cssRGBPrefixPattern             = regexp.MustCompile(`(?i)^rgba?:`)
)

// ParseOscColorResponse parses an OSC 10, 11, or 4 color reply (upstream
// parseOscColorResponse). It returns ok=false when data is not such a reply.
func ParseOscColorResponse(data string) (OscColorResponse, bool) {
	match := oscColorResponsePattern.FindStringSubmatch(data)
	if match == nil {
		return OscColorResponse{}, false
	}
	target := OscColorTarget{}
	switch {
	case match[1] == "10":
		target.Foreground = true
	case match[1] == "11":
		target.Background = true
	default:
		index, _ := strconv.Atoi(match[2])
		target.Index = index
	}
	rgb, ok := parseOscColorValue(match[3])
	return OscColorResponse{Target: target, RGB: rgb, HasRGB: ok}, true
}

// IsOsc11BackgroundColorResponse reports whether data is an OSC 11 reply.
func IsOsc11BackgroundColorResponse(data string) bool {
	response, ok := ParseOscColorResponse(data)
	return ok && response.Target.Background
}

// ParseOsc11BackgroundColor parses an OSC 11 background-color reply.
func ParseOsc11BackgroundColor(data string) (RgbColor, bool) {
	response, ok := ParseOscColorResponse(data)
	if !ok || !response.Target.Background || !response.HasRGB {
		return RgbColor{}, false
	}
	return response.RGB, true
}

// parseOscColorValue parses an OSC color payload: #rrggbb, #rrrrggggbbbb, or
// rgb:/rgba: channels.
func parseOscColorValue(rawValue string) (RgbColor, bool) {
	value := strings.TrimSpace(rawValue)
	if strings.HasPrefix(value, "#") {
		hex := value[1:]
		if len(hex) == 6 && oscHexChannelPattern.MatchString(hex) {
			r, _ := strconv.ParseInt(hex[0:2], 16, 32)
			g, _ := strconv.ParseInt(hex[2:4], 16, 32)
			b, _ := strconv.ParseInt(hex[4:6], 16, 32)
			return RgbColor{R: int(r), G: int(g), B: int(b)}, true
		}
		if len(hex) == 12 && oscHexChannelPattern.MatchString(hex) {
			r, okR := parseOscHexChannel(hex[0:4])
			g, okG := parseOscHexChannel(hex[4:8])
			b, okB := parseOscHexChannel(hex[8:12])
			if !okR || !okG || !okB {
				return RgbColor{}, false
			}
			return RgbColor{R: r, G: g, B: b}, true
		}
		return RgbColor{}, false
	}

	rgbValue := cssRGBPrefixPattern.ReplaceAllString(value, "")
	parts := strings.Split(rgbValue, "/")
	if len(parts) < 3 {
		return RgbColor{}, false
	}
	r, okR := parseOscHexChannel(parts[0])
	g, okG := parseOscHexChannel(parts[1])
	b, okB := parseOscHexChannel(parts[2])
	if !okR || !okG || !okB {
		return RgbColor{}, false
	}
	return RgbColor{R: r, G: g, B: b}, true
}

// ParseTerminalColorSchemeReport parses a `CSI ? 997 ; n n` color-scheme report.
func ParseTerminalColorSchemeReport(data string) (TerminalColorScheme, bool) {
	match := colorSchemeReportPattern.FindStringSubmatch(data)
	if match == nil {
		return "", false
	}
	if match[1] == "2" {
		return TerminalColorSchemeLight, true
	}
	return TerminalColorSchemeDark, true
}

func parseOscHexChannel(channel string) (int, bool) {
	if !oscHexChannelPattern.MatchString(channel) {
		return 0, false
	}
	max := math.Pow(16, float64(len(channel))) - 1
	if max <= 0 {
		return 0, false
	}
	parsed, err := strconv.ParseInt(channel, 16, 64)
	if err != nil {
		return 0, false
	}
	return int(math.Round(float64(parsed) / max * 255)), true
}
