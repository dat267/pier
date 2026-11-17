package interactive

import (
	"testing"

	"github.com/dat267/pier/coding"
)

// SettingsWiring's optional sinks are nil-checked so a wiring without a UI or
// transcript still applies settings. Inverting a check calls a nil func or a nil
// container and panics.
func TestSettingsWiringGuardsOptionalSinks(t *testing.T) {
	settings := coding.NewInMemorySettingsManager(&coding.Settings{}, coding.SettingsManagerCreateOptions{})
	wiring := &SettingsWiring{Settings: settings}
	wiring.showStatus("ignored")
	wiring.requestRender()

	callbacks := wiring.BuildSettingsCallbacks(func() {}, func() {})
	callbacks.OnMermaidRenderingModeChange("off")
	callbacks.OnHideThinkingBlockChange(true)

	renders := 0
	wiring.RequestRender = func() { renders++ }
	wiring.requestRender()
	if renders != 1 {
		t.Fatalf("renders = %d, want the configured callback", renders)
	}
}
