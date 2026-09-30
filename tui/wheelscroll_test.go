package tui

import (
	"encoding/json"
	"testing"
)

func scrollLineCounts(accelerator *WheelScrollAccelerator, times []float64, direction int) []int {
	out := make([]int, 0, len(times))
	for _, at := range times {
		out = append(out, accelerator.Next(direction, at))
	}
	return out
}

func wheelLinesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Port of packages/tui/test/wheel-scroll.test.ts (upstream f1927c2d5).
func TestWheelScrollAcceleratorFixedLines(t *testing.T) {
	accelerator := newWheelScrollAccelerator(FixedWheelScrollLines(3), true)
	if got := scrollLineCounts(accelerator, []float64{0, 10, 20, 1000}, 1); !wheelLinesEqual(got, []int{3, 3, 3, 3}) {
		t.Fatalf("fixed lines = %v", got)
	}
	accelerator.SetLines(FixedWheelScrollLines(0))
	if got := accelerator.Next(1, 2000); got != 1 {
		t.Fatalf("fixed 0 lines = %d (want 1)", got)
	}
}

func TestWheelScrollAcceleratorAutoOnAcceleratingTerminal(t *testing.T) {
	accelerator := newWheelScrollAccelerator(AutoWheelScrollLines(), false)
	if got := scrollLineCounts(accelerator, []float64{0, 10, 20, 30}, 1); !wheelLinesEqual(got, []int{1, 1, 1, 1}) {
		t.Fatalf("non-accelerating terminal = %v", got)
	}
}

func TestWheelScrollAcceleratorScalesWithVelocity(t *testing.T) {
	accelerator := newWheelScrollAccelerator(AutoWheelScrollLines(), true)
	if got := scrollLineCounts(accelerator, []float64{0, 150, 300, 450}, 1); !wheelLinesEqual(got, []int{1, 1, 1, 1}) {
		t.Fatalf("100ms+ gaps = %v", got)
	}
	if got := scrollLineCounts(accelerator, []float64{1000, 1050, 1100, 1150}, 1); !wheelLinesEqual(got, []int{1, 2, 2, 2}) {
		t.Fatalf("50ms gaps = %v", got)
	}
	if got := scrollLineCounts(accelerator, []float64{2000, 2020, 2040, 2060}, 1); !wheelLinesEqual(got, []int{1, 5, 5, 5}) {
		t.Fatalf("20ms gaps = %v", got)
	}
	if got := scrollLineCounts(accelerator, []float64{3000, 3010, 3020, 3030}, 1); !wheelLinesEqual(got, []int{1, 6, 6, 6}) {
		t.Fatalf("10ms gaps = %v", got)
	}
}

func TestWheelScrollAcceleratorBurstStaysOneLine(t *testing.T) {
	accelerator := newWheelScrollAccelerator(AutoWheelScrollLines(), true)
	if got := scrollLineCounts(accelerator, []float64{0, 3, 6, 9}, 1); !wheelLinesEqual(got, []int{1, 1, 1, 1}) {
		t.Fatalf("burst = %v", got)
	}
}

func TestWheelScrollAcceleratorResetsOnDirectionChangeAndPause(t *testing.T) {
	accelerator := newWheelScrollAccelerator(AutoWheelScrollLines(), true)
	if got := scrollLineCounts(accelerator, []float64{0, 20, 40}, 1); !wheelLinesEqual(got, []int{1, 5, 5}) {
		t.Fatalf("direction run = %v", got)
	}
	if got := accelerator.Next(-1, 60); got != 1 {
		t.Fatalf("direction change = %d (want 1)", got)
	}
	if got := scrollLineCounts(accelerator, []float64{500, 520}, 1); !wheelLinesEqual(got, []int{1, 5}) {
		t.Fatalf("after pause = %v", got)
	}
}

func TestWheelScrollAcceleratorCarriesFractionalLines(t *testing.T) {
	accelerator := newWheelScrollAccelerator(AutoWheelScrollLines(), true)
	if got := scrollLineCounts(accelerator, []float64{0, 40, 80, 120, 160}, 1); !wheelLinesEqual(got, []int{1, 2, 3, 2, 3}) {
		t.Fatalf("carry = %v", got)
	}
}

// TestWheelScrollLinesJSONRoundTrip pins the settings encoding: "auto" stays a
// string, a fixed count stays a bare integer (byte parity with settings.json).
func TestWheelScrollLinesJSONRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		value WheelScrollLines
		want  string
	}{
		{AutoWheelScrollLines(), `"auto"`},
		{FixedWheelScrollLines(3), `3`},
	} {
		encoded, err := json.Marshal(tc.value)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != tc.want {
			t.Fatalf("marshal %#v = %s (want %s)", tc.value, encoded, tc.want)
		}
		var decoded WheelScrollLines
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded != tc.value {
			t.Fatalf("round trip %s = %#v (want %#v)", encoded, decoded, tc.value)
		}
	}
}
