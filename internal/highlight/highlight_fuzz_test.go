package highlight

// Every supported language tokenizes arbitrary text without an error or a
// panic; the highlighters are regex-driven and an empty match is a classic
// index bug.

import "testing"

func FuzzHighlightWith(f *testing.F) {
	for _, seed := range []string{"", "x", "func main() {}", "```", "\x00\x1b", "a:b", "'unterminated", "/*"} {
		f.Add(seed, "go")
	}
	f.Fuzz(func(t *testing.T, code, language string) {
		languages := append(Languages(), language)
		for _, lang := range languages {
			if !SupportsLanguage(lang) {
				continue
			}
			if _, err := HighlightWith(code, lang, true); err != nil {
				t.Fatalf("language %q: %v", lang, err)
			}
		}
	})
}
