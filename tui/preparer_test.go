package tui

import "testing"

// The deferred-transcript pre-render warms a component's render cache off the
// UI loop: Prepare must leave the next Render at the same width a cache hit,
// and a different width must re-render (the cache is keyed by width).

func TestMarkdownPrepareWarmsRenderCache(t *testing.T) {
	theme := mdTestTheme()
	text := "Line with `code` and **bold** text and a [link](https://example.com).\n"
	markdown := NewMarkdown(text, 1, 0, theme, nil, MarkdownOptions{})

	markdown.Prepare(80)
	before := markdown.renderedTokens
	if before == 0 {
		t.Fatal("Prepare did not render any tokens")
	}
	if lines := markdown.Render(80); len(lines) == 0 {
		t.Fatal("Render returned nothing after Prepare")
	}
	if markdown.renderedTokens != before {
		t.Fatalf("Render(80) after Prepare(80) re-rendered %d tokens", markdown.renderedTokens-before)
	}

	// A new width must not reuse the width-80 cache.
	_ = markdown.Render(70)
	if markdown.renderedTokens == before {
		t.Fatal("Render at a new width reused the warmed cache")
	}
}

func TestContainerPrepareRecursesToMarkdown(t *testing.T) {
	theme := mdTestTheme()
	markdown := NewMarkdown("hello **world**", 0, 0, theme, nil, MarkdownOptions{})
	container := &Container{}
	container.AddChild(markdown)

	container.Prepare(80)
	before := markdown.renderedTokens
	_ = container.Render(80)
	if markdown.renderedTokens != before {
		t.Fatalf("Container.Render after Prepare re-rendered %d tokens", markdown.renderedTokens-before)
	}
}

// renderCounter counts renders so the test can pin that Prepare skips a child
// with no cacheable subtree instead of rendering it.
type renderCounter struct{ renders int }

func (c *renderCounter) Render(int) []string { c.renders++; return nil }

func (*renderCounter) Invalidate() {}

func TestPrepareSkipsNonPreparers(t *testing.T) {
	child := &renderCounter{}
	container := &Container{}
	container.AddChild(child)
	container.Prepare(80)
	if child.renders != 0 {
		t.Fatalf("Prepare rendered a non-preparer %d times", child.renders)
	}
}
