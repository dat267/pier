package interactive

import "testing"

// ModelWiring's optional sinks are nil-checked so a headless wiring can call
// them safely (the port's error and render helpers). Inverting the checks calls
// a nil func and panics.
func TestModelWiringGuardsOptionalSinks(t *testing.T) {
	wiring := &ModelWiring{}
	wiring.showWarning("ignored")
	wiring.showError("ignored")
	wiring.requestRender()

	var warnings []string
	wiring.ShowWarning = func(message string) { warnings = append(warnings, message) }
	wiring.showWarning("warned")
	if len(warnings) != 1 || warnings[0] != "warned" {
		t.Fatalf("warnings = %v", warnings)
	}

	renders := 0
	wiring.RequestRender = func() { renders++ }
	wiring.requestRender()
	if renders != 1 {
		t.Fatalf("renders = %d, want the configured callback", renders)
	}
}
