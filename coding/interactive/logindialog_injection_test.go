package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// D460: an authorization server's text is data, not terminal control. The
// reference prints the instructions, the device user code and an info message
// as they arrive; pier sanitizes remote text (D342).
func TestLoginDialogDropsProviderTerminalControl(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	component := NewLoginDialogComponent(nil, nil, "xai", nil, "", "")
	render := func() string { return strings.Join(component.Render(120), "\n") }
	expectClean := func(stage string, wants ...string) {
		t.Helper()
		rendered := render()
		for _, injected := range []string{"]0;owned", "]8;;https://evil", "\x1b[2J", "\x1b[31m"} {
			if strings.Contains(rendered, injected) {
				t.Fatalf("%s: provider control %q reached the dialog: %q", stage, injected, rendered)
			}
		}
		for _, want := range wants {
			if !strings.Contains(rendered, want) {
				t.Fatalf("%s: dialog dropped %q: %q", stage, want, rendered)
			}
		}
	}

	// ShowAuth replaces the body, so each stage renders on its own.
	component.ShowAuth("https://x.ai/authorize", "line one\n\x1b[2Jline two")
	expectClean("auth", "line one", "line two")

	component.ShowDeviceCode("https://x.ai/device", "\x1b]0;owned\x07ABCD-1234")
	expectClean("device", "ABCD-1234")

	component.ShowInfo("\x1b]8;;https://evil\x07click", []ai.AuthInfoLink{{URL: "https://x.ai/help", Label: "\x1b[31mhelp\x1b[0m"}}, false)
	expectClean("info", "click", "help")
}
