package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/dat267/pier/tui"
)

// D221: the last terminal colors are persisted so the system theme is generated
// from the previous session's terminal before the OSC query replies. The
// query still revalidates and overwrites the cache; the cache only removes the
// one-frame mismatch on startup. tui.TerminalColors has no JSON tags (it is an
// internal type), so the file shape is defined here.

// TerminalColorsCacheFileName is the agent-dir file holding the last terminal
// colors (OSC 10/11/4 replies).
const TerminalColorsCacheFileName = "terminal-colors.json"

// terminalColorsCache is the on-disk shape.
type terminalColorsCache struct {
	Foreground *tuiRgb  `json:"foreground,omitempty"`
	Background *tuiRgb  `json:"background,omitempty"`
	Palette    []tuiRgb `json:"palette,omitempty"`
}

type tuiRgb struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

func rgbToCache(color tui.RgbColor) tuiRgb {
	return tuiRgb{R: color.R, G: color.G, B: color.B}
}

func (c tuiRgb) toRgb() tui.RgbColor {
	return tui.RgbColor{R: c.R, G: c.G, B: c.B}
}

func terminalColorsCacheFrom(colors tui.TerminalColors) terminalColorsCache {
	cached := terminalColorsCache{}
	if colors.Foreground != nil {
		fg := rgbToCache(*colors.Foreground)
		cached.Foreground = &fg
	}
	if colors.Background != nil {
		bg := rgbToCache(*colors.Background)
		cached.Background = &bg
	}
	for _, color := range colors.Palette {
		cached.Palette = append(cached.Palette, rgbToCache(color))
	}
	return cached
}

func (c terminalColorsCache) toTerminalColors() tui.TerminalColors {
	colors := tui.TerminalColors{}
	if c.Foreground != nil {
		fg := c.Foreground.toRgb()
		colors.Foreground = &fg
	}
	if c.Background != nil {
		bg := c.Background.toRgb()
		colors.Background = &bg
	}
	for _, color := range c.Palette {
		colors.Palette = append(colors.Palette, color.toRgb())
	}
	return colors
}

func terminalColorsCacheEmpty(colors tui.TerminalColors) bool {
	return colors.Foreground == nil && colors.Background == nil && len(colors.Palette) == 0
}

// LoadSystemTerminalColorsCache seeds the system theme's terminal colors from
// the agent dir's cache. It reports whether a usable cache was loaded. Call it
// at boot before the first theme is built; a missing or malformed file is not an
// error (the live query fills the colors in a moment later).
func LoadSystemTerminalColorsCache(agentDir string) bool {
	if agentDir == "" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(agentDir, TerminalColorsCacheFileName))
	if err != nil {
		return false
	}
	var cached terminalColorsCache
	if err := json.Unmarshal(raw, &cached); err != nil {
		return false
	}
	colors := cached.toTerminalColors()
	if terminalColorsCacheEmpty(colors) {
		return false
	}
	SetSystemTerminalColors(colors)
	return true
}

// saveTerminalColorsCache writes the cache. Callers must run it off the owner
// loop (the combined-query wait goroutine does). Empty colors are not written,
// so a query that reported nothing cannot erase a previous cache.
func saveTerminalColorsCache(agentDir string, colors tui.TerminalColors) {
	if agentDir == "" || terminalColorsCacheEmpty(colors) {
		return
	}
	raw, err := json.Marshal(terminalColorsCacheFrom(colors))
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(agentDir, TerminalColorsCacheFileName), raw, 0o600)
}
