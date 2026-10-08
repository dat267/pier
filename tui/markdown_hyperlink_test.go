package tui

import (
	"strings"
	"testing"
)

// The OSC 8 branch is reachable only when the installed capabilities say the terminal
// forwards hyperlinks, so this covers both sides of it. Before the setter existed
// nothing wrote the state, which left the branch unreachable and always rendered the
// legacy `text (url)` form (upstream markdown.ts link handling).
func TestMarkdownLinksFollowInstalledCapabilities(t *testing.T) {
	previous := GetTerminalCapabilities()
	t.Cleanup(func() { SetTerminalCapabilities(previous) })

	render := func(capabilities TerminalCapabilities) string {
		SetTerminalCapabilities(capabilities)
		markdown := NewMarkdown("[text](http://x)", 0, 0, mdTestTheme(), nil, MarkdownOptions{})
		return strings.Join(markdown.Render(40), "\n")
	}

	plain := render(TerminalCapabilities{})
	if strings.Contains(plain, "\x1b]8;;") {
		t.Fatalf("a terminal without hyperlink support got an OSC 8 link: %q", plain)
	}
	if !strings.Contains(plain, "http://x") {
		t.Fatalf("the legacy form dropped the URL: %q", plain)
	}

	linked := render(TerminalCapabilities{Hyperlinks: true})
	if !strings.Contains(linked, "\x1b]8;;http://x\x1b\\") {
		t.Fatalf("a hyperlink terminal got no OSC 8 link: %q", linked)
	}
	if !strings.Contains(linked, "\x1b]8;;\x1b\\") {
		t.Fatalf("the OSC 8 link was not closed: %q", linked)
	}
	if strings.Contains(linked, "(http://x)") {
		t.Fatalf("the OSC 8 form still printed the URL twice: %q", linked)
	}
}
