package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// render-utils.ts:170-180 (pi v1.5.0 f9a26365) returns
// linkPath(theme.fg("accent", shortenPath(value)), value, cwd), and linkPath
// (:15-19) wraps that text in a pathToFileURL hyperlink when the terminal
// reports hyperlinks, resolving a relative path against cwd first. The port
// took cwd and ignored it, so a path rendered identically with hyperlinks on
// and off and was never clickable (D436).
func TestRenderToolPathLinksWhenHyperlinksAreOn(t *testing.T) {
	InitTheme("dark", false)
	previous := tui.GetTerminalCapabilities()
	t.Cleanup(func() { tui.SetTerminalCapabilities(previous) })
	theme := ActiveTheme()

	raw := "/home/dat/repos/pier/coding/paths.go"
	tui.SetTerminalCapabilities(tui.TerminalCapabilities{Hyperlinks: false})
	plain := RenderToolPath(&raw, theme, "/home/dat/repos/pier", "")
	if strings.Contains(plain, "\x1b]8;;") {
		t.Fatalf("hyperlinks off still emitted a link: %q", plain)
	}

	tui.SetTerminalCapabilities(tui.TerminalCapabilities{Hyperlinks: true})
	linked := RenderToolPath(&raw, theme, "/home/dat/repos/pier", "")
	if !strings.HasPrefix(linked, "\x1b]8;;file:///home/dat/repos/pier/coding/paths.go") {
		t.Errorf("path is not a file:// link: %q", linked)
	}
	if !strings.Contains(linked, plain) {
		t.Errorf("link dropped the styled path: %q does not contain %q", linked, plain)
	}

	// A relative path resolves against cwd before it becomes a URL.
	rel := "coding/paths.go"
	resolved := RenderToolPath(&rel, theme, "/home/dat/repos/pier", "")
	if !strings.Contains(resolved, "file:///home/dat/repos/pier/coding/paths.go") {
		t.Errorf("relative path not resolved against cwd: %q", resolved)
	}
}
