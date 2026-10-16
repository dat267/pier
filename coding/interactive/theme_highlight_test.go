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

// Syntax highlighting is not built in (D226): a known language renders exactly
// like an unknown one, one mdCodeBlock color for the whole fence and no token
// colors. This is upstream's own no-valid-language fallback, applied to every
// language.
func TestHighlightCodeRendersEveryLanguagePlainly(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	InitTheme("dark", false)

	source := "func answer() int { return 42 }"
	got := HighlightCode(source, "go")
	want := []string{ActiveTheme().Fg("mdCodeBlock", source)}
	if !slices.Equal(got, want) {
		t.Fatalf("known-language highlight = %q, want plain %q", got, want)
	}
	if coding.StripAnsi(got[0]) != source {
		t.Fatalf("highlighted source = %q", coding.StripAnsi(got[0]))
	}
	if strings.Count(got[0], "\x1b[38;2;") != 1 {
		t.Fatalf("known language applied token colors: %q", got[0])
	}
}

// Every line of a multi-line fence is styled and returned in order.
func TestHighlightCodeStylesEveryLine(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	InitTheme("dark", false)

	got := HighlightCode("a\nb", "go")
	if len(got) != 2 {
		t.Fatalf("lines = %d, want 2", len(got))
	}
	for i, line := range got {
		if coding.StripAnsi(line) != []string{"a", "b"}[i] {
			t.Fatalf("line %d = %q", i, coding.StripAnsi(line))
		}
	}
}
