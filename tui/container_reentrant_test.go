package tui

import (
	"strconv"
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

// The reported crash was "index out of range [364] with length 0": the same
// borrowed-cache read as D375, at the row index of a long transcript. A
// following child at that index must keep its admitted lines when the streaming
// child's retry callback clears the parent cache mid-render.
func TestContainerKeepsCachedRowsDeepInALongTranscript(t *testing.T) {
	parent := &Container{SkipUnchangedChildren: true}
	for index := range 363 {
		parent.AddChild(NewText("filler "+strconv.Itoa(index), 0, 0, nil))
	}
	markdown := NewMarkdown("previous frame", 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
	tail := &Container{}
	tail.AddChild(NewText("cached following row", 0, 0, nil))
	parent.AddChild(markdown)
	parent.AddChild(tail)
	if got := len(parent.Render(80)); got == 0 {
		t.Fatal("first frame is empty")
	}

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
