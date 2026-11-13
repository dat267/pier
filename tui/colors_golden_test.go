package tui

// Replays the upstream color goldens. The generator (tui/testdata/colors/
// generate.mjs) drives the pin's colorToHex, colorToOklch, colorToOkhsl,
// foregroundAnsi and mixColors over an OKLCH grid, every indexed color and
// fractional RGB fixtures. Chromatic channels are compared exactly through the
// hex and ANSI forms; the OKHSL/OKLCH channels use a tolerance because the hue
// of an achromatic color is atan2 of denormal noise and differs between engines.

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

type colorGoldenSpec struct {
	Type  string  `json:"type"`
	Index int     `json:"index"`
	R     float64 `json:"r"`
	G     float64 `json:"g"`
	B     float64 `json:"b"`
	L     float64 `json:"l"`
	C     float64 `json:"c"`
	H     float64 `json:"h"`
}

func (s colorGoldenSpec) color(t *testing.T) Color {
	t.Helper()
	var (
		color Color
		err   error
	)
	switch s.Type {
	case "indexed":
		color, err = IndexedColor(s.Index)
	case "rgb":
		color, err = NewRgbColor(s.R, s.G, s.B)
	case "oklch":
		color, err = NewOklchColor(s.L, s.C, s.H)
	default:
		t.Fatalf("unknown color spec %q", s.Type)
	}
	if err != nil {
		t.Fatalf("build color %+v: %v", s, err)
	}
	return color
}

func TestColorGoldensMatchTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/colors/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var records []struct {
		Kind      string          `json:"kind"`
		Spec      colorGoldenSpec `json:"spec"`
		First     colorGoldenSpec `json:"first"`
		Second    colorGoldenSpec `json:"second"`
		Space     string          `json:"space"`
		Amount    float64         `json:"amount"`
		Hex       string          `json:"hex"`
		ANSI256   string          `json:"ansi256"`
		Truecolor string          `json:"truecolor"`
		H         float64         `json:"h"`
		S         float64         `json:"s"`
		L         float64         `json:"l"`
		C         float64         `json:"c"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("golden is empty")
	}
	const tolerance = 1e-9
	for _, record := range records {
		switch record.Kind {
		case "color":
			color := record.Spec.color(t)
			if got := ColorToHex(color); got != record.Hex {
				t.Errorf("ColorToHex(%+v) = %q, want %q", record.Spec, got, record.Hex)
			}
			if got := ForegroundAnsi(color, TerminalColorMode256); got != record.ANSI256 {
				t.Errorf("256-color ANSI(%+v) = %q, want %q", record.Spec, got, record.ANSI256)
			}
			if got := ForegroundAnsi(color, TerminalColorModeTruecolor); got != record.Truecolor {
				t.Errorf("truecolor ANSI(%+v) = %q, want %q", record.Spec, got, record.Truecolor)
			}
		case "okhsl":
			got := ColorToOkhsl(record.Spec.color(t))
			if math.Abs(got.L-record.L) > tolerance || math.Abs(got.S-record.S) > tolerance {
				t.Errorf("ColorToOkhsl(%+v) = {%v %v}, want L %v S %v", record.Spec, got.H, got.S, record.L, record.S)
			}
			if (got.S > tolerance || record.S > tolerance) && math.Abs(got.H-record.H) > tolerance {
				t.Errorf("ColorToOkhsl(%+v) hue = %v, want %v", record.Spec, got.H, record.H)
			}
		case "oklch":
			got := ColorToOklch(record.Spec.color(t))
			if math.Abs(got.L-record.L) > tolerance || math.Abs(got.C-record.C) > tolerance {
				t.Errorf("ColorToOklch(%+v) = {%v %v}, want L %v C %v", record.Spec, got.L, got.C, record.L, record.C)
			}
			if (got.C > tolerance || record.C > tolerance) && math.Abs(got.H-record.H) > tolerance {
				t.Errorf("ColorToOklch(%+v) hue = %v, want %v", record.Spec, got.H, record.H)
			}
		case "mix":
			space := ColorMixOklch
			if record.Space == "srgb" {
				space = ColorMixSrgb
			}
			mixed, err := MixColors(record.First.color(t), record.Second.color(t), record.Amount, space)
			if err != nil {
				t.Fatalf("MixColors(%+v, %+v, %v, %q): %v", record.First, record.Second, record.Amount, record.Space, err)
			}
			if got := ColorToHex(mixed); got != record.Hex {
				t.Errorf("MixColors(%+v, %+v, %v, %q) = %q, want %q", record.First, record.Second, record.Amount, record.Space, got, record.Hex)
			}
		default:
			t.Fatalf("unknown golden record %q", record.Kind)
		}
	}
}
