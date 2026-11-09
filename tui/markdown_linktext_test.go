package tui

import (
	"strings"
	"testing"
)

// Text that is not a link must not lose its tail. pi v1.5.0 (f9a26365)
// packages/tui/src/components/markdown.ts delegates to marked 18.0.11, whose
// inline `link` rule needs a destination without ASCII whitespace and, if a
// title follows, one of the three quoted or parenthesized forms. Pier used to
// parse `[docs](https://x.example/a b)` as a link whose destination was
// `https://x.example/a` and whose title was `b`, so the URL was rewritten and
// the message showed `docs (https://x.example/a)` instead of the literal text
// with a bare autolink inside it (A437).
func TestMarkdownKeepsNonLinkTextVisible(t *testing.T) {
	theme := mdTestTheme()
	rendered := strings.Join(mdTrimmedLines(NewMarkdown("see [docs](https://x.example/a b) now", 0, 0, theme, nil, MarkdownOptions{}), 200), "\n")
	for _, want := range []string{"[docs](", "https://x.example/a", " b) now"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered %q is missing %q", rendered, want)
		}
	}
	// The URL display form is only added for a link whose text differs from its
	// destination, so the bogus `(https://x.example/a)` suffix is gone.
	if strings.Contains(rendered, "</link><url>") {
		t.Errorf("rendered %q still shows a link URL suffix", rendered)
	}
}

// A well-formed link still renders as one, with the escaped punctuation in its
// destination resolved (A438).
func TestMarkdownRendersLinkWithUnescapedDestination(t *testing.T) {
	theme := mdTestTheme()
	rendered := strings.Join(mdTrimmedLines(NewMarkdown("[t](u\\)v)", 0, 0, theme, nil, MarkdownOptions{}), 40), "\n")
	if rendered != "<link><u>t</u></link><url> (u)v)</url>" {
		t.Errorf("got %q", rendered)
	}
}
