package tui

// Reference: pi v1.5.2 (b4afdddf) packages/tui/src/colors.ts. A concrete color's
// channels stay unrounded until they are written: `colorToRgb` returns the
// fractional channels of an oklch color and the stored channels of an rgb color,
// `mixColors` interpolates those, and only `colorToHex`/`colorAnsi`/`rgbToAnsi256`
// round. Pier's RgbColor carries integers, so mixing used to round each channel
// first: a chained sRGB mix then lands one step off (191.25 rounds to 191 in the
// reference, 191.5 to 192 after rounding the input).

import (
	"math"
	"testing"
)

func TestSrgbMixingKeepsUnroundedChannels(t *testing.T) {
	black, err := NewRgbColor(0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	white, err := NewRgbColor(255, 255, 255)
	if err != nil {
		t.Fatal(err)
	}
	red, err := NewRgbColor(255, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	middle, err := MixColors(black, white, 0.5, ColorMixSrgb)
	if err != nil {
		t.Fatal(err)
	}
	if got := ColorToHex(middle); got != "#808080" {
		t.Fatalf("midpoint hex = %q, want #808080", got)
	}
	chained, err := MixColors(middle, red, 0.5, ColorMixSrgb)
	if err != nil {
		t.Fatal(err)
	}
	if got := ColorToHex(chained); got != "#bf4040" {
		t.Fatalf("chained mix hex = %q, want #bf4040", got)
	}
}

func TestFractionalChannelsSurviveConversion(t *testing.T) {
	half, err := NewRgbColor(47.5, 47.5, 47.5)
	if err != nil {
		t.Fatal(err)
	}
	red, err := NewRgbColor(255, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := MixColors(half, red, 0.5, ColorMixSrgb)
	if err != nil {
		t.Fatal(err)
	}
	if got := ColorToHex(mixed); got != "#971818" {
		t.Fatalf("mixed hex = %q, want #971818", got)
	}
	base, err := NewOklchColor(0.5, 0.1, 200)
	if err != nil {
		t.Fatal(err)
	}
	inOklch, err := MixColors(half, base, 0.35, ColorMixOklch)
	if err != nil {
		t.Fatal(err)
	}
	if got := ColorToHex(inOklch); got != "#2a4748" {
		t.Fatalf("oklch mix hex = %q, want #2a4748", got)
	}
	if got, want := ColorToOkhsl(half).L, 0.20604904405565316; math.Abs(got-want) > 1e-12 {
		t.Fatalf("okhsl lightness = %v, want %v", got, want)
	}
	if got, want := ColorToOklch(half).L, 0.30719007915498286; math.Abs(got-want) > 1e-12 {
		t.Fatalf("oklch lightness = %v, want %v", got, want)
	}
	if got, want := ForegroundAnsi(half, TerminalColorMode256), "\x1b[38;5;236m"; got != want {
		t.Fatalf("ansi = %q, want %q", got, want)
	}
}
