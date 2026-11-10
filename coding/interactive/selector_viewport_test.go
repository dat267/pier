package interactive

import (
	"context"
	"fmt"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// pi v1.5.0 selectors capture fullscreen page keys and move their own selection
// instead of scrolling the transcript behind them.
func TestSelectorsCaptureViewportInput(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	models := make([]*ai.Model, 25)
	for i := range models {
		models[i] = &ai.Model{ID: fmt.Sprintf("model-%02d", i), Name: fmt.Sprintf("Model %02d", i), Provider: "test"}
	}
	pageDown := "\x1b[6~"
	pageUp := "\x1b[5~"

	modelSelector := NewModelSelectorComponent(nil, nil, nil, &fakeModelRuntime{models: models}, nil, nil, nil, "", nil, nil)
	defer modelSelector.Dispose()
	if !modelSelector.CapturesViewportInput() {
		t.Fatal("model selector does not capture page keys")
	}
	modelSelector.HandleInput(pageDown)
	if modelSelector.selectedIndex != 10 {
		t.Fatalf("model PageDown = %d, want 10", modelSelector.selectedIndex)
	}
	modelSelector.HandleInput(pageUp)
	if modelSelector.selectedIndex != 0 {
		t.Fatalf("model PageUp = %d, want 0", modelSelector.selectedIndex)
	}

	providers := make([]AuthSelectorProvider, 20)
	for i := range providers {
		providers[i] = AuthSelectorProvider{ID: fmt.Sprintf("p%02d", i), Name: fmt.Sprintf("P%02d", i)}
	}
	oauthSelector := NewOAuthSelectorComponent("login", providers, nil, nil, "")
	if !oauthSelector.CapturesViewportInput() {
		t.Fatal("OAuth selector does not capture page keys")
	}
	oauthSelector.HandleInput(pageDown)
	if oauthSelector.selectedIndex != 8 {
		t.Fatalf("OAuth PageDown = %d, want 8", oauthSelector.selectedIndex)
	}

	scopedSelector := NewScopedModelsSelectorComponent(ModelsConfig{AllModels: models}, ModelsCallbacks{})
	if !scopedSelector.CapturesViewportInput() {
		t.Fatal("scoped-models selector does not capture page keys")
	}
	scopedSelector.HandleInput(pageDown)
	if scopedSelector.selectedIndex != 8 {
		t.Fatalf("scoped PageDown = %d, want 8", scopedSelector.selectedIndex)
	}

	empty := func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) { return nil, nil }
	sessionSelector := NewSessionSelectorComponent(empty, empty, nil, nil, nil, nil, SessionSelectorOptions{})
	if !sessionSelector.CapturesViewportInput() {
		t.Fatal("session selector does not capture page keys")
	}
	sessionSelector.mode = "rename"
	if sessionSelector.CapturesViewportInput() {
		t.Fatal("rename input must let page keys through")
	}
}
