package tui

import (
	"encoding/json"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// WheelScrollLines is the fullscreen wheel setting: Auto (accelerate fast spins)
// or a fixed line count. Port of WheelScrollLines in packages/tui/src/
// wheel-scroll.ts (upstream f1927c2d5).
type WheelScrollLines struct {
	Auto  bool
	Lines int
}

// AutoWheelScrollLines accelerates fast wheel spins on terminals that do not
// already accelerate them.
func AutoWheelScrollLines() WheelScrollLines { return WheelScrollLines{Auto: true} }

// FixedWheelScrollLines is a fixed number of lines per wheel event.
func FixedWheelScrollLines(lines int) WheelScrollLines { return WheelScrollLines{Lines: lines} }

// MarshalJSON writes "auto" or the bare integer, the settings.json form.
func (w WheelScrollLines) MarshalJSON() ([]byte, error) {
	if w.Auto {
		return []byte(`"auto"`), nil
	}
	return []byte(strconv.Itoa(w.Lines)), nil
}

// UnmarshalJSON accepts "auto" or a number.
func (w *WheelScrollLines) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == `"auto"` {
		*w = AutoWheelScrollLines()
		return nil
	}
	var number float64
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*w = FixedWheelScrollLines(int(number))
	return nil
}

const (
	// Several events closer than this belong to one physical notch (Ghostty
	// emits them ~4 ms apart) or come from a high-resolution source: one line
	// each, no acceleration.
	wheelBurstGapMs = 5.0
	// A pause longer than this ends a scroll gesture.
	wheelGestureGapMs = 200.0
	// Average event gap that maps to one line per event; faster scales up.
	wheelReferenceGapMs = 100.0
	wheelMaxAutoLines   = 6.0
)

// terminalAcceleratesWheel reports whether the terminal already accelerates
// wheel and trackpad input: a local macOS terminal does, other platforms and
// SSH sessions do not (upstream terminalAcceleratesWheel).
func terminalAcceleratesWheel() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	for _, key := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if os.Getenv(key) != "" {
			return false
		}
	}
	return true
}

// WheelScrollAccelerator converts wheel events into line counts. Port of
// WheelScrollAccelerator.
type WheelScrollAccelerator struct {
	lines      WheelScrollLines
	accelerate bool
	lastTime   float64
	lastDir    int
	hasAverage bool
	averageGap float64
	carry      float64
}

// NewWheelScrollAccelerator creates an accelerator that accelerates unless the
// host terminal already does.
func NewWheelScrollAccelerator(lines WheelScrollLines) *WheelScrollAccelerator {
	return newWheelScrollAccelerator(lines, !terminalAcceleratesWheel())
}

func newWheelScrollAccelerator(lines WheelScrollLines, accelerate bool) *WheelScrollAccelerator {
	return &WheelScrollAccelerator{lines: lines, accelerate: accelerate, lastTime: math.Inf(-1)}
}

// SetLines changes the setting and resets the gesture state.
func (a *WheelScrollAccelerator) SetLines(lines WheelScrollLines) {
	a.lines = lines
	a.reset()
}

// Next returns the positive line count for one wheel event in direction
// (-1 up, 1 down) at now (monotonic milliseconds).
func (a *WheelScrollAccelerator) Next(direction int, now float64) int {
	if !a.lines.Auto {
		return max(1, a.lines.Lines)
	}
	if !a.accelerate {
		return 1
	}
	gap := now - a.lastTime
	sameGesture := direction == a.lastDir && gap <= wheelGestureGapMs
	a.lastTime = now
	a.lastDir = direction
	if !sameGesture {
		a.hasAverage = false
		a.carry = 0
		return 1
	}
	if gap < wheelBurstGapMs {
		return 1
	}
	if a.hasAverage {
		a.averageGap = (a.averageGap + gap) / 2
	} else {
		a.averageGap = gap
		a.hasAverage = true
	}
	lines := math.Min(wheelMaxAutoLines, math.Max(1, wheelReferenceGapMs/a.averageGap)) + a.carry
	whole := math.Floor(lines)
	a.carry = lines - whole
	return int(whole)
}

func (a *WheelScrollAccelerator) reset() {
	a.lastTime = math.Inf(-1)
	a.lastDir = 0
	a.hasAverage = false
	a.averageGap = 0
	a.carry = 0
}
