package interactive

import "testing"

// The production composition validates its required seams once: a forgotten
// assignment must fail at construction, not silently during a session (the
// queue reporters were once dropped, so their statuses vanished).
func TestAppWiringValidation(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	if missing := app.validateWiring(); len(missing) != 0 {
		t.Fatalf("a fully built app reports missing wiring: %v", missing)
	}

	app.events.ShowError = nil
	missing := app.validateWiring()
	if len(missing) != 1 || missing[0] != "events.ShowError" {
		t.Fatalf("validator = %v, want [events.ShowError]", missing)
	}

	app.events.ShowError = func(string) {}
	if missing := app.validateWiring(); len(missing) != 0 {
		t.Fatalf("restoring the seam still reports %v", missing)
	}
}
