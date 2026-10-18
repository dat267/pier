package interactive

import (
	"strings"
	"testing"
)

// A streaming turn re-renders the whole assistant message per delta, and the
// markdown renderer re-highlights a code fence inside the re-lex window even
// when the fence text did not change. The Oct 18 streak-start dump caught the
// loop inside highlight.HighlightWith (regex backtracking) at a 100ms stall, so
// repeated highlighting of identical code must be a cache hit.
func TestHighlightCodeCachesRepeatedBlocks(t *testing.T) {
	newRendererTestTheme(t)
	code := strings.Repeat("func f(x int) int { return x + 1 } // a comment\n", 60)

	first := HighlightCode(code, "go")
	if len(first) == 0 {
		t.Fatal("no highlighted lines")
	}
	second := HighlightCode(code, "go")
	if len(second) != len(first) {
		t.Fatalf("cache hit length = %d, want %d", len(second), len(first))
	}
	if &first[0] != &second[0] {
		t.Fatal("repeated highlight recomputed instead of hitting the cache")
	}
}

// The cache must not serve stale colors after a theme change.
func TestHighlightCodeCacheClearsOnThemeChange(t *testing.T) {
	newRendererTestTheme(t)
	code := strings.Repeat("func f() int { return 1 }\n", 40)
	first := HighlightCode(code, "go")

	SetThemeInstance(mustLoadTheme("light"))
	second := HighlightCode(code, "go")
	if len(second) == 0 {
		t.Fatal("no lines after theme change")
	}
	if &first[0] == &second[0] {
		t.Fatal("theme change reused the previous theme's highlighted lines")
	}
}

// An unsupported language falls back to plain coloring and is never cached.
func TestHighlightCodeUnsupportedLanguage(t *testing.T) {
	newRendererTestTheme(t)
	lines := HighlightCode("some text", "definitely-not-a-language")
	if len(lines) == 0 {
		t.Fatal("plain fallback produced no lines")
	}
}
