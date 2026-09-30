package interactive

import "github.com/dat267/pier/tui"

// ThemedText is a tui.Text whose content applies theme colors. Plain tui.Text
// keeps the colors its string was built with, so a theme change, or the system
// theme receiving the terminal's colors, would leave it stale. This rebuilds the
// string from build after every invalidation (upstream
// modes/interactive/components/themed-text.ts, bf8e4b953).
//
// build must return the same content each time apart from colors; snapshot
// changing data before creating the component, or invalidate it when the state
// it reads changes.
type ThemedText struct {
	*tui.Text
	build func() string
	stale bool
}

// NewThemedText creates a themed text component.
func NewThemedText(build func() string, paddingX int, paddingY int) *ThemedText {
	return &ThemedText{Text: tui.NewText("", paddingX, paddingY, nil), build: build, stale: true}
}

// Invalidate marks the content for a rebuild on the next render.
func (t *ThemedText) Invalidate() {
	t.Text.Invalidate()
	t.stale = true
}

// Render rebuilds the text when a theme change invalidated it.
func (t *ThemedText) Render(width int) []string {
	if t.stale {
		t.stale = false
		t.Text.SetText(t.build())
	}
	return t.Text.Render(width)
}
