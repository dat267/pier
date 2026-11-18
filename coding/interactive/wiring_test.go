package interactive

import (
	"context"
	"errors"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// A seam audit found func-typed wiring fields that no production code assigned.
// Most were deliberate (test seams, host hooks, D41 vestiges) and are documented
// where they live, one (ConfigureHTTPIdleTimeout) was a dispatcher seam with
// nothing safe to install (D254), and one was a real gap, pinned here.

// The global debug key is the renderer's: upstream matches shift+ctrl+d in
// tui.ts and interactive-mode.ts points ui.onDebug at handleDebugCommand, so the
// key and /debug do the same work. Neither hook was assigned here.
func TestDebugKeyRunsTheDebugCommand(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir) // the debug log path follows the env
	app, cleanup := newTestApp(t)
	defer cleanup()

	screen, ok := tuiConcrete(app.ui).(*tui.MainScreen)
	if !ok {
		t.Fatalf("renderer = %T", tuiConcrete(app.ui))
	}
	if screen.MatchesDebugKey == nil || screen.OnDebug == nil {
		t.Fatal("the renderer's debug key is not wired")
	}
	if screen.MatchesDebugKey("x") {
		t.Error("MatchesDebugKey accepted a plain character")
	}

	// The callback is the same work /debug does: a debug log in the agent dir.
	screen.OnDebug()
	if _, err := os.Stat(filepath.Join(agentDir, coding.AppName+"-debug.log")); err != nil {
		t.Fatalf("the debug key wrote no debug log: %v", err)
	}
}

// pi v1.5.0, model-search.ts getDistinctModelName: isCovered tests
// `word.length > 1`, JavaScript string length, so a single astral letter counts
// as two and can be covered by a compact id. Counting runes made such a name
// distinct when the reference hides it.
func TestDistinctModelNameCountsUTF16Units(t *testing.T) {
	if name, ok := GetDistinctModelName("a𝔘b", "𝔘"); ok {
		t.Fatalf("name %q reported distinct, want the astral letter covered by the compact id", name)
	}
	if name, ok := GetDistinctModelName("a𝔘b", "x"); !ok || name != "x" {
		t.Fatalf("name %q ok=%v, want a distinct name", name, ok)
	}
}

// Reference: pi v1.4.0 (4a080252), oauth-selector.ts filterProviders includes
// provider.method.name. The real /login options carry *ai.OAuthAuth methods.
func TestOAuthSelectorSearchesAuthMethodName(t *testing.T) {
	wiring, _ := newAuthTestWiring(t)
	providers := wiring.GetLoginProviderOptions("")
	for _, tc := range []struct {
		query string
		match bool
	}{
		{"ChatGPT", true},
		{"OpenAI", true},
		{"not-a-real-login-method", false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			selected := ""
			selector := NewOAuthSelectorComponent("login", providers,
				func(id, authType string) { selected = id }, nil, tc.query)
			selector.HandleInput("\r")
			if (selected != "") != tc.match {
				rendered := tui.StripTerminalSequences(strings.Join(selector.Render(80), "\n"))
				t.Fatalf("query %q selected %q, want match=%v; rendered:\n%s", tc.query, selected, tc.match, rendered)
			}
		})
	}
}

// pi 4a080252 components/scoped-models-selector.ts applies its refresh result
// on the JS event loop, so dismissal and the in-flight refresh never touch the
// same state concurrently. Pier's refresh worker reads the dismissal flag while
// the owner writes it; this exercises that handoff under -race.
func TestScopedModelsRefreshObservesDismissalSafely(t *testing.T) {
	models := []*ai.Model{{ID: "a", Provider: "p"}}
	finished := make(chan struct{})
	runtime := &modelTestRuntime{models: models, refresh: func() (ai.ModelsRefreshResult, error) {
		close(finished)
		return ai.ModelsRefreshResult{}, errors.New("offline")
	}}
	wiring, _, _ := newModelTestWiring(t, runtime)
	wiring.ShowModelsSelector(context.Background())
	<-finished
	// The refresh has settled, so the worker is about to re-read the dismissal
	// flag while the owner sets it. No channel orders those two accesses, so
	// -race reports the unsynchronized read.
	wiring.Slot.DisposeActiveSelector()
	for range 2 {
		wiring.UI.(*tui.MainScreen).RenderNow(true)
	}
}
