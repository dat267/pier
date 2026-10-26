package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), modes/interactive/theme/theme.ts
// (loadThemeFromPath -> new Theme) and its callers, which wrap theme loads in
// try/catch (theme.ts addCustomThemes, the theme watcher and setTheme). A theme
// file with a bad color or a missing/circular variable must therefore be
// reported, not crash the process: in Go a panic in the watcher goroutine or on
// the UI loop kills the whole session (CR-070).
func writeBrokenThemeFile(t *testing.T, dir, file string, accent any, vars map[string]any) {
	t.Helper()
	colors := map[string]any{}
	for _, key := range requiredThemeColors {
		colors[key] = "#abcdef"
	}
	colors["accent"] = accent
	document := map[string]any{"name": "broken", "colors": colors}
	if vars != nil {
		document["vars"] = vars
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadThemeFromPathReportsBrokenThemes(t *testing.T) {
	for _, item := range []struct {
		name   string
		accent any
		vars   map[string]any
		want   string
	}{
		{"missing variable", "nope", nil, "Variable reference not found: nope"},
		{"circular variables", "a", map[string]any{"a": "b", "b": "a"}, "Circular variable reference detected"},
		{"self reference", "a", map[string]any{"a": "a"}, "Circular variable reference detected"},
		{"bad hex", "#12345", nil, "Invalid color value"},
	} {
		t.Run(item.name, func(t *testing.T) {
			dir := t.TempDir()
			writeBrokenThemeFile(t, dir, "broken.json", item.accent, item.vars)
			theme, err := LoadThemeFromPath(filepath.Join(dir, "broken.json"), ColorModeTruecolor)
			if err == nil || theme != nil {
				t.Fatalf("LoadThemeFromPath = (%v, %v), want a reported error", theme, err)
			}
			if !strings.Contains(err.Error(), item.want) {
				t.Fatalf("error = %q, want it to contain %q", err, item.want)
			}
		})
	}
}

// Discovery runs when the theme selector opens, so a broken user theme must not
// take the app down there either.
func TestCustomThemeInfosSkipsBrokenThemes(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	t.Cleanup(func() { SetCustomThemesDir("") })
	writeThemeFile(t, dir, "good.json", "good", "#123456")
	writeBrokenThemeFile(t, dir, "broken.json", "nope", nil)

	if got := customThemeInfos(); len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("themes = %+v, want only the good one", got)
	}
	// Loading the broken theme by its declared name reports an error too.
	if _, err := loadTheme("broken", ColorModeTruecolor); err == nil {
		t.Fatal("loadTheme(broken) succeeded, want an error")
	}
}
