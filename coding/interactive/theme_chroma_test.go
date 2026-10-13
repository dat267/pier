package interactive

import (
	"slices"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

func TestHighlightCodeFallsBackForUnknownLanguage(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	InitTheme("dark", false)

	got := HighlightCode("plain text", "not-a-real-language")
	want := []string{ActiveTheme().Fg("mdCodeBlock", "plain text")}
	if !slices.Equal(got, want) {
		t.Fatalf("unknown-language highlight = %q, want %q", got, want)
	}
}

func TestHighlightCodeAppliesTokenColorsForKnownLanguage(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	InitTheme("dark", false)

	lines := HighlightCode("func answer() int { return 42 }", "go")
	if len(lines) != 1 {
		t.Fatalf("highlighted line count = %d, want 1", len(lines))
	}
	if got := coding.StripAnsi(lines[0]); got != "func answer() int { return 42 }" {
		t.Fatalf("highlighted source = %q", got)
	}
	if strings.Count(lines[0], "\x1b[38;2;") < 3 {
		t.Fatalf("highlighted output has fewer than three token colors: %q", lines[0])
	}
}
