package interactive

import "testing"

// A ThemeSubmenu without callbacks may still preview: the nil check must come
// first (inverting it dereferences a nil pointer).
func TestThemeSubmenuPreviewWithoutCallbacks(t *testing.T) {
	(&ThemeSubmenu{}).themePreview("dark")

	var previews []string
	callbacks := &SettingsCallbacks{OnThemePreview: func(value string) { previews = append(previews, value) }}
	(&ThemeSubmenu{callbacks: callbacks}).themePreview("light")
	if len(previews) != 1 || previews[0] != "light" {
		t.Fatalf("previews = %v", previews)
	}
}

// A single-mode submenu reports its chosen theme, and automatic mode follows the
// terminal's appearance (theme.ts themeSetting / activeAutomaticTheme).
func TestThemeSubmenuThemeSettingAndAppearance(t *testing.T) {
	single := &ThemeSubmenu{mode: "single", singleTheme: "solarized"}
	if got := single.themeSetting(); got != "solarized" {
		t.Fatalf("single themeSetting = %q", got)
	}
	automatic := &ThemeSubmenu{mode: "automatic", lightTheme: "light-theme", darkTheme: "dark-theme", terminalTheme: TerminalThemeLight}
	if got := automatic.themeSetting(); got != "light-theme/dark-theme" {
		t.Fatalf("automatic themeSetting = %q", got)
	}
	if got := automatic.activeAutomaticTheme(); got != "light-theme" {
		t.Fatalf("activeAutomaticTheme = %q", got)
	}
	dark := &ThemeSubmenu{mode: "automatic", lightTheme: "light-theme", darkTheme: "dark-theme", terminalTheme: TerminalThemeDark}
	if got := dark.activeAutomaticTheme(); got != "dark-theme" {
		t.Fatalf("dark activeAutomaticTheme = %q", got)
	}
}
