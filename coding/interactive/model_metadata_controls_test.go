package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// D342: pi f9a26365 components/{model-selector,scoped-models-selector}.ts
// display provider catalog fields as-is.
// The port's existing untrusted-terminal-text policy also applies to those
// fields, without changing the identity passed to a selection callback.
func TestScopedModelSelectorCatalogFieldsCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	model := &ai.Model{ID: "m" + unsafeClearScreen + "odel", Name: "Friendly" + unsafeClipboard + " Name", Provider: "p" + unsafeCursorHome + "rovider"}
	for _, available := range []bool{true, false} {
		config := ModelsConfig{AllModels: []*ai.Model{model}}
		if !available {
			config.AllModels = nil
			config.EnabledModelIDs = &EnabledIds{IDs: []string{model.Provider + "/" + model.ID}}
		}
		selector := NewScopedModelsSelectorComponent(config, ModelsCallbacks{})
		rendered := strings.Join(selector.Render(160), "\n")
		for _, sequence := range []string{unsafeClearScreen, unsafeClipboard, unsafeCursorHome} {
			if strings.Contains(rendered, sequence) {
				t.Errorf("available=%v: catalog control %q reached scoped selector output", available, sequence)
			}
		}
		if !strings.Contains(rendered, "model") || !strings.Contains(rendered, "provider") {
			t.Errorf("available=%v: catalog display lost ordinary text", available)
		}
	}
}

// Footer model IDs come from the same catalog, not trusted renderer markup.
// D141 uses the port's one-line footer; the pinned components/footer.ts also
// displays catalog model IDs, subject to the port's D342 sanitization policy.
func TestFooterCatalogIDCannotEmitTerminalControls(t *testing.T) {
	for _, sequence := range []string{unsafeClearScreen, unsafeClipboard, unsafeCursorHome} {
		got := FooterLine(FooterInput{ModelID: "m" + sequence + "odel", Cwd: "/project"}, 160)
		if strings.Contains(got, sequence) {
			t.Errorf("catalog control %q reached footer output", sequence)
		}
		if !strings.Contains(got, "model") {
			t.Errorf("footer lost ordinary model ID text: %q", got)
		}
	}
}

func TestModelSelectorCatalogFieldsCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	model := &ai.Model{ID: "m" + unsafeClearScreen + "odel", Name: "Friendly" + unsafeClipboard + " Name", Provider: "p" + unsafeCursorHome + "rovider"}
	var selected *ai.Model
	selector := NewModelSelectorComponent(nil, func(func()) {}, nil, &fakeModelRuntime{models: []*ai.Model{model}}, nil,
		func(value *ai.Model) { selected = value }, nil, "", nil, nil)
	defer selector.HandleInput("\x1b")
	rendered := strings.Join(selector.Render(160), "\n")
	for _, sequence := range []string{unsafeClearScreen, unsafeClipboard, unsafeCursorHome} {
		if strings.Contains(rendered, sequence) {
			t.Errorf("catalog control %q reached model selector output", sequence)
		}
	}
	for _, text := range []string{"model", "Friendly Name", "provider"} {
		if !strings.Contains(rendered, text) {
			t.Errorf("catalog display lost ordinary text %q", text)
		}
	}
	selector.HandleInput("\r")
	if selected != model || selected.ID != "m"+unsafeClearScreen+"odel" {
		t.Fatal("display sanitization changed selected model identity")
	}
}
