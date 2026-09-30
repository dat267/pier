package interactive

import (
	"strings"
	"testing"
)

// Port of the ThemedText case in upstream themed-text.test.ts (bf8e4b953):
// content is built lazily and rebuilt with the current theme after invalidation.
func TestThemedTextRebuildsOnInvalidate(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	builds := 0
	text := NewThemedText(func() string {
		builds++
		return ActiveTheme().Fg("accent", "hello")
	}, 1, 1)
	if builds != 0 {
		t.Fatalf("built eagerly (%d builds)", builds)
	}
	dark := strings.Join(text.Render(20), "")

	InitTheme("light", false)
	if got := strings.Join(text.Render(20), ""); got != dark {
		t.Fatalf("recolored without an invalidate")
	}
	text.Invalidate()
	light := strings.Join(text.Render(20), "")
	if light == dark {
		t.Fatal("did not recolor after invalidate")
	}
	if !strings.Contains(light, ActiveTheme().GetFgAnsi("accent")) {
		t.Fatalf("rebuilt text used a stale accent: %q", light)
	}
	if builds != 2 {
		t.Fatalf("builds = %d (want 2)", builds)
	}
}
