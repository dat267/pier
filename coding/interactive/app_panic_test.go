package interactive

import (
	"strings"
	"testing"
)

// Upstream installs a process uncaughtException handler that restores the
// terminal, records the crash and exits 1 (interactive-mode.ts uncaughtCrash).
// Go has no process-level hook, so the owner loop's entry point converts an
// escaping panic into the same handled crash instead of dying with the terminal
// still in raw mode on the alternate screen.
func TestAppRunConvertsAPanicIntoAnUncaughtCrash(t *testing.T) {
	lifecycle, _, exits, _ := newLifecycleTest(t)
	var recorded string
	var stderr strings.Builder
	lifecycle.options.Exit = func(code int) { *exits = append(*exits, code) }
	lifecycle.options.WriteErr = func(text string) { stderr.WriteString(text) }
	lifecycle.options.RecordCrash = func(kind string, err error) bool {
		recorded = kind + ": " + err.Error()
		return true
	}
	app := &App{lifecycle: lifecycle}
	app.runGuarded(func() { panic("boom") })
	if len(*exits) != 1 || (*exits)[0] != 1 {
		t.Fatalf("exits = %v, want one exit 1", *exits)
	}
	if !strings.Contains(recorded, "uncaught_exception") || !strings.Contains(recorded, "boom") {
		t.Fatalf("recorded crash = %q", recorded)
	}
	if !strings.Contains(stderr.String(), "exiting due to uncaughtException") || !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// A clean run is unchanged, and the panic value reaches the recorder as an
// error even though Go panics carry any value.
func TestAppRunReturnsNormallyWithoutAPanic(t *testing.T) {
	lifecycle, _, exits, _ := newLifecycleTest(t)
	app := &App{lifecycle: lifecycle}
	ran := false
	app.runGuarded(func() { ran = true })
	if !ran || len(*exits) != 0 {
		t.Fatalf("ran=%v exits=%v", ran, *exits)
	}
}
