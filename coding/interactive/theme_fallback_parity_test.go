package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/internal/offloop"
)

// pi v1.4.0 theme/theme.ts initTheme/setTheme and theme-controller.ts
// applyThemeName fall back to system, not dark, if a selected theme is missing.
func TestInvalidThemeFallsBackToSystem(t *testing.T) {
	for _, entry := range []string{"initial", "switch", "controller", "queued-controller"} {
		t.Run(entry, func(t *testing.T) {
			SetCustomThemesDir(t.TempDir())
			SetRegisteredThemes(nil)
			InitTheme("light", false)
			const missing = "missing-parity-theme"
			switch entry {
			case "initial":
				InitTheme(missing, false)
			case "switch":
				if success, _ := SetTheme(missing, false); success {
					t.Fatal("missing theme was accepted")
				}
			default:
				var message string
				options := ThemeControllerOptions{
					UI:        &fakeThemeUI{},
					ShowError: func(value string) { message = value },
				}
				if entry == "queued-controller" {
					options.ThemeQueue = offloop.New()
				}
				controller := NewInteractiveThemeController(options)
				defer controller.Dispose()
				controller.SetThemeSetting(missing)
				controller.ThemeQueueFlushForTest()
				if got := controller.ActiveThemeName(); got != SystemThemeName {
					t.Errorf("controller fallback = %q, want system", got)
				}
				if !strings.Contains(message, "Fell back to the system theme.") {
					t.Errorf("missing system fallback diagnostic: %q", message)
				}
			}
			if got := CurrentThemeName(); got != SystemThemeName {
				t.Fatalf("fallback = %q, want system", got)
			}
		})
	}
}
