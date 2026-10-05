package interactive

import (
	"testing"

	"github.com/dat267/pier/tui"
)

func TestConfigureLowBandwidthDoesNotEnableOverSSH(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 55000")
	t.Setenv("PIER_LOW_BANDWIDTH", "1")
	defer tui.SetLowBandwidth(false)
	tui.SetLowBandwidth(true)
	if ConfigureLowBandwidth() {
		t.Fatal("low-bandwidth rendering enabled over SSH")
	}
	if tui.LowBandwidth() {
		t.Fatal("SSH boot left low-bandwidth renderer enabled")
	}
}
