package interactive

import "github.com/dat267/pier/tui"

// ConfigureLowBandwidth is kept as a no-op for source compatibility. Upstream
// pi does not switch its renderer into a low-bandwidth mode over SSH.
func ConfigureLowBandwidth() bool {
	tui.SetLowBandwidth(false)
	return false
}
