package tui

import (
	"strings"
	"testing"
)

// D375: upstream f9a26365 packages/tui/src/tui.ts renders synchronously.
// Our cache skip path must preserve its admitted lines when a built-in
// Markdown retry callback invalidates the parent during that same render.
func TestContainerKeepsCachedRowsWhenChildRequestsRetry(t *testing.T) {
	parent := &Container{SkipUnchangedChildren: true}
	markdown := NewMarkdown("previous frame", 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
	tail := &Container{}
	tail.AddChild(NewText("cached following row", 0, 0, nil))
	parent.AddChild(markdown)
	parent.AddChild(tail)
	parent.Render(80)

	markdown.SetPreparation(&MarkdownPreparation{
		Submit:        func(func() func()) bool { return false },
		RequestRender: parent.MarkDirty,
	})
	markdown.SetText(largeMarkdownFixture())
	got := strings.Join(parent.Render(80), "\n")
	if !strings.Contains(got, "previous frame") || !strings.Contains(got, "cached following row") {
		t.Fatalf("retry lost a retained row: %q", got)
	}
}
