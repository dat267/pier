package tui

import "sync/atomic"

// lowBandwidthEnabled is the process-wide low-bandwidth render toggle.
var lowBandwidthEnabled atomic.Bool

// SetLowBandwidth toggles the low-bandwidth render path for explicit renderer
// clients. Interactive startup leaves it disabled to match upstream pi.
func SetLowBandwidth(enabled bool) { lowBandwidthEnabled.Store(enabled) }

// LowBandwidth reports whether the low-bandwidth render path is active.
func LowBandwidth() bool { return lowBandwidthEnabled.Load() }
