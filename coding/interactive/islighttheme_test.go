package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) modes/interactive/theme/theme.ts isLightTheme
// loads the named theme (or the current one) and reports whether its appearance
// is light. Pier compared the name against "light", so a custom light theme,
// the current theme, and the system theme all read as dark.
func TestIsLightThemeResolvesTheThemeAppearance(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	SetRegisteredThemes(nil)
	t.Cleanup(func() {
		SetCustomThemesDir("")
		SetRegisteredThemes(nil)
		StopThemeWatcher()
	})
	colors := map[string]any{}
	for _, key := range requiredThemeColors {
		colors[key] = "#101010"
	}
	document := map[string]any{"name": "paperlight", "appearance": "light", "colors": colors}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "paperlight.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsLightTheme("light") {
		t.Fatal("the light theme must be light")
	}
	if IsLightTheme("dark") {
		t.Fatal("the dark theme must not be light")
	}
	if !IsLightTheme("paperlight") {
		t.Fatal("a custom theme declaring a light appearance must be light")
	}

	// The empty name resolves the current theme.
	InitTheme("light", false)
	if !IsLightTheme("") {
		t.Fatal("the current light theme must be light when the name is empty")
	}
	InitTheme("dark", false)
	if IsLightTheme("") {
		t.Fatal("the current dark theme must not be light when the name is empty")
	}
}
