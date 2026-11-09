package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// The fallback box for a custom entry whose renderer panicked interpolates the
// entry's customType and the recovered panic text straight into the frame. Both
// are session data: customType is persisted in the session file, and a panic
// message can quote the input that caused it. Every neighbouring fallback
// sanitizes (assistantmessage.go's error block, D342).
func TestCustomEntryFallbackSanitizesItsText(t *testing.T) {
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	renderer := func(CustomEntry, EntryRenderOptions, *Theme) tui.Component {
		panic("boom\x1b]0;pwned\x07")
	}
	component := NewCustomEntryComponent(CustomEntry{CustomType: "note\x1b]0;typed\x07"}, renderer)
	rendered := strings.Join(component.Render(100), "\n")
	for _, sequence := range []string{"\x1b]0;pwned", "\x1b]0;typed"} {
		if strings.Contains(rendered, sequence) {
			t.Fatalf("the fallback let %q reach the terminal: %q", sequence, rendered)
		}
	}
	if !strings.Contains(rendered, "renderer failed") {
		t.Fatalf("the fallback did not report the failure: %q", rendered)
	}
}
