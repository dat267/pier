package tui

import (
	"math"
	"regexp"
	"testing"
)

// Port of packages/tui/test/colors.test.ts (upstream 567469096).

func colorsClose(a, b Color) bool {
	if a.Kind != b.Kind {
		return false
	}
	const epsilon = 1e-9
	closeTo := func(x, y float64) bool { return math.Abs(x-y) < epsilon }
	switch a.Kind {
	case ColorKindIndexed:
		return a.Index == b.Index
	case ColorKindRGB:
		return closeTo(a.R, b.R) && closeTo(a.G, b.G) && closeTo(a.B, b.B)
	default:
		return closeTo(a.L, b.L) && closeTo(a.C, b.C) && closeTo(a.H, b.H)
	}
}

func mustParseColor(t *testing.T, value string) Color {
	t.Helper()
	color, err := ParseColor(value)
	if err != nil {
		t.Fatalf("ParseColor(%q): %v", value, err)
	}
	return color
}

func TestParseColorHexAndOklch(t *testing.T) {
	if got, want := mustParseColor(t, "#abc"), (Color{Kind: ColorKindRGB, R: 170, G: 187, B: 204}); !colorsClose(got, want) {
		t.Errorf("parseColor(#abc) = %+v, want %+v", got, want)
	}
	if got, want := mustParseColor(t, "oklch(62% 0.1 200)"), (Color{Kind: ColorKindOklch, L: 0.62, C: 0.1, H: 200}); !colorsClose(got, want) {
		t.Errorf("parseColor(oklch) = %+v, want %+v", got, want)
	}
	for _, invalid := range []string{"", "red"} {
		if _, err := ParseColor(invalid); err == nil {
			t.Errorf("ParseColor(%q) did not fail", invalid)
		}
	}
}

func TestOklchGamutMapping(t *testing.T) {
	cases := []struct {
		l, c, h float64
		want    RgbColor
	}{
		{0.627955, 0.257683, 29.2339, RgbColor{R: 255, G: 0, B: 0}},
		{1, 0.3, 150, RgbColor{R: 255, G: 255, B: 255}},
		{0, 0.3, 150, RgbColor{R: 0, G: 0, B: 0}},
	}
	for _, tc := range cases {
		color, err := NewOklchColor(tc.l, tc.c, tc.h)
		if err != nil {
			t.Fatal(err)
		}
		if got := ColorToRgb(color); got != tc.want {
			t.Errorf("colorToRgb(oklch(%v %v %v)) = %+v, want %+v", tc.l, tc.c, tc.h, got, tc.want)
		}
	}
}

func TestParseOkhslAndRoundTrip(t *testing.T) {
	if got, want := mustParseColor(t, "okhsl(29.23 100% 56.8%)"), mustParseColorFromOkhsl(t, 29.23, 1, 0.568); !colorsClose(got, want) {
		t.Errorf("parseColor(okhsl) = %+v, want %+v", got, want)
	}
	if got, want := mustParseColor(t, "OKHSL(250deg 60% 55%)"), mustParseColorFromOkhsl(t, 250, 0.6, 0.55); !colorsClose(got, want) {
		t.Errorf("parseColor(OKHSL) = %+v, want %+v", got, want)
	}
	if _, err := ParseColor("okhsl(250 160% 55%)"); err == nil {
		t.Error("out-of-range saturation did not fail")
	}
	for _, hex := range []string{"#4f8eb3", "#20242a", "#f8f9fa"} {
		channels := ColorToOkhsl(mustParseColor(t, hex))
		color, err := NewOkhslColor(channels.H, channels.S, channels.L)
		if err != nil {
			t.Fatal(err)
		}
		if got := ColorToHex(color); got != hex {
			t.Errorf("okhsl round-trip %s = %s", hex, got)
		}
	}
}

func mustParseColorFromOkhsl(t *testing.T, h, s, l float64) Color {
	t.Helper()
	color, err := NewOkhslColor(h, s, l)
	if err != nil {
		t.Fatal(err)
	}
	return color
}

func TestTerminalDetectsTrueColor(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"COLORTERM truecolor", map[string]string{"COLORTERM": "truecolor"}, true},
		{"COLORTERM 24bit", map[string]string{"COLORTERM": "24BIT"}, true},
		{"TERM -direct", map[string]string{"TERM": "xterm-direct"}, true},
		{"TERM 256color", map[string]string{"TERM": "xterm-256color"}, false},
		{"nothing", map[string]string{}, false},
	}
	for _, tc := range cases {
		got := TerminalDetectsTrueColor(func(key string) string { return tc.env[key] })
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStyleTextClosesInReverseOrder(t *testing.T) {
	fg, _ := NewRgbColor(18, 52, 86)
	bg, _ := IndexedColor(9)
	got := StyleText("Ready", TextStyle{
		Fg:             &fg,
		Bg:             &bg,
		TextAttributes: TextAttributes{Bold: true, Italic: true},
	}, TerminalColorModeTruecolor)
	want := "\x1b[38;2;18;52;86m\x1b[48;5;9m\x1b[1m\x1b[3mReady\x1b[23m\x1b[22m\x1b[49m\x1b[39m"
	if got != want {
		t.Errorf("styleText = %q, want %q", got, want)
	}
	got = StyleText("Ready", TextStyle{Fg: &fg}, TerminalColorMode256)
	if !regexp.MustCompile(`^\x1b\[38;5;\d+mReady\x1b\[39m$`).MatchString(got) {
		t.Errorf("styleText 256color = %q", got)
	}
}
