package interactive

import "testing"

// footer.ts calls truncateToWidth (pi v1.5.0 f9a26365 tui/src/utils.ts:1186),
// whose ellipsis branch returns a text that already fits instead of clipping
// the ellipsis: at width 2, "ab" stays "ab" while "日本語" becomes "..". The
// port short-circuited every width <= 3 to the ellipsis, so a fitting footer
// value was replaced by dots (D433).
func TestFooterTruncateKeepsFittingText(t *testing.T) {
	for _, tc := range []struct {
		text string
		w    int
		want string
	}{
		{"ab", 2, "ab"},
		{"a", 2, "a"},
		{"a", 1, "a"},
		{"", 2, ""},
		{"abc", 3, "abc"},
		{"abc", 4, "abc"},
		{"abcdef", 5, "ab..."},
		// Text wider than the budget still collapses to the ellipsis.
		{"abc", 2, ".."},
		{"日本語", 2, ".."},
		{"日本語", 3, "..."},
	} {
		if got := FooterTruncate(tc.text, tc.w); got != tc.want {
			t.Errorf("FooterTruncate(%q, %d) = %q, want %q", tc.text, tc.w, got, tc.want)
		}
	}
}
