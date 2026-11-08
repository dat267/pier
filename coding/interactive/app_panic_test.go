package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// Upstream installs a process uncaughtException handler that restores the
// terminal, records the crash and exits 1 (interactive-mode.ts uncaughtCrash).
// Go has no process-level hook, so the owner loop's entry point converts an
// escaping panic into the same handled crash instead of dying with the terminal
// still in raw mode on the alternate screen.
func TestAppRunConvertsAPanicIntoAnUncaughtCrash(t *testing.T) {
	lifecycle, _, exits, _ := newLifecycleTest(t)
	var recorded string
	var recordedError error
	var stderr strings.Builder
	lifecycle.options.Exit = func(code int) { *exits = append(*exits, code) }
	lifecycle.options.WriteErr = func(text string) { stderr.WriteString(text) }
	lifecycle.options.RecordCrash = func(kind string, err error) bool {
		recorded = kind + ": " + err.Error()
		recordedError = err
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
	stacked, ok := recordedError.(interface{ StackTrace() string })
	if !ok || !strings.Contains(stacked.StackTrace(), "runGuarded") {
		t.Fatalf("recorded error has no panic stack: %#v", recordedError)
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

type widthBreakingComponent struct{ wide bool }

func (c *widthBreakingComponent) Render(width int) []string {
	if c.wide {
		return []string{strings.Repeat("x", width+5)}
	}
	return []string{"ok"}
}
func (c *widthBreakingComponent) HandleInput(string) {}
func (c *widthBreakingComponent) Invalidate()        {}

// Upstream's main-screen renderer throws on a line wider than the terminal
// (pi v1.5.0 (f9a26365) tui-main-screen.ts doRender), and interactive mode's
// uncaughtException handler restores the terminal, prints the error, records
// the crash and exits 1 (interactive-mode.ts uncaughtCrash). The port's
// renderer reports the error through OnRenderError after stopping the
// terminal, but the app never set it, so pier kept running with the terminal
// restored and nothing drawn.
func TestOverwideMainScreenLineIsAnUncaughtCrash(t *testing.T) {
	lifecycle, _, exits, terminal := newLifecycleTest(t)
	var stderr strings.Builder
	recorded := ""
	lifecycle.options.WriteErr = func(text string) { stderr.WriteString(text) }
	lifecycle.options.RecordCrash = func(kind string, err error) bool {
		recorded = kind
		return false
	}
	app := &App{lifecycle: lifecycle}
	screen := tui.NewMainScreen(terminal, false, t.TempDir())
	screen.DisableAutoRender()
	lifecycle.options.UI = screen
	app.installRenderErrorHandler(screen)
	component := &widthBreakingComponent{}
	screen.AddChild(component)
	screen.Start()
	screen.RenderNow(false)
	component.wide = true
	screen.RenderNow(false)
	if len(*exits) != 1 || (*exits)[0] != 1 {
		t.Fatalf("exits = %v, want one exit 1", *exits)
	}
	if recorded != "uncaught_exception" {
		t.Fatalf("recorded crash kind = %q", recorded)
	}
	if !strings.Contains(stderr.String(), "exiting due to uncaughtException") || !strings.Contains(stderr.String(), "exceeds terminal width") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
