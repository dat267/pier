package tui

import (
	"reflect"
	"strings"
	"testing"
)

// D204: requesting an already-cached width supersedes in-flight work too.
func TestMarkdownPreparationRejectsSupersededCachedWidth(t *testing.T) {
	text := strings.Repeat("# Heading\n\nparagraph\n\n", 4000)
	markdown := NewMarkdown(text, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
	old := markdown.Render(80)
	var work func() func()
	markdown.SetPreparation(&MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }, RequestRender: func() {}})
	markdown.Render(81)
	apply := work()
	markdown.Render(80)
	apply()
	work = nil
	if got := markdown.Render(80); !reflect.DeepEqual(got, old) || work != nil {
		t.Fatal("superseded width overwrote cached current frame")
	}
}

func TestDetachedMarkdownPrepareRemainsSynchronous(t *testing.T) {
	text := strings.Repeat("# Heading\n\nparagraph\n\n", 4000)
	markdown := NewMarkdown(text, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
	markdown.SetPreparation(&MarkdownPreparation{Submit: func(func() func()) bool { t.Error("detached Prepare recursively submitted another task"); return false }, RequestRender: func() {}})
	markdown.Prepare(80)
	if lines := markdown.Render(80); len(lines) <= 1 {
		t.Fatal("detached Prepare did not populate complete cache")
	}
}

// D204: both content changes and explicit style/theme invalidation supersede
// older completions, even if completions are delivered in reverse order.
func TestMarkdownPreparationRejectsStaleContentAndTheme(t *testing.T) {
	for _, change := range []string{"content", "theme"} {
		t.Run(change, func(t *testing.T) {
			text := strings.Repeat("# Heading\n\nparagraph\n\n", 4000)
			markdown := NewMarkdown(text, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
			var work []func() func()
			markdown.SetPreparation(&MarkdownPreparation{Submit: func(run func() func()) bool { work = append(work, run); return true }, RequestRender: func() {}})
			markdown.Render(80)
			if change == "content" {
				markdown.SetText(strings.ReplaceAll(text, "Heading", "new heading"))
			} else {
				markdown.Theme.Heading = func(s string) string { return "NEW:" + s }
				markdown.Invalidate()
			}
			markdown.Render(80)
			old, new := work[0](), work[1]()
			new()
			old()
			want := NewMarkdown(markdown.Text, 0, 0, markdown.Theme, nil, MarkdownOptions{}).Render(80)
			if got := markdown.Render(80); !reflect.DeepEqual(got, want) {
				t.Fatal("old content/theme completion replaced current rendering")
			}
		})
	}
}

func TestMarkdownPreparationKeepsLatestEmptyFrame(t *testing.T) {
	markdown := NewMarkdown("old frame", 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
	markdown.Render(80)
	markdown.SetText("")
	markdown.Render(80)
	markdown.SetPreparation(&MarkdownPreparation{Submit: func(func() func()) bool { return true }, RequestRender: func() {}})
	markdown.SetText(strings.Repeat("paragraph\n\n", 7000))
	if lines := markdown.Render(80); len(lines) != 0 {
		t.Fatal("preparation resurrected content from before an empty frame")
	}
}

// D204: a cold large Markdown must submit a detached immutable render, not
// execute parsing/transforms on the owner. Its completed frame is byte-identical.
func TestLargeMarkdownPreparationLeavesOwnerRenderingCheap(t *testing.T) {
	text := strings.Repeat("# Heading\n\nparagraph\n\n", 4000)
	transformed := 0
	options := MarkdownOptions{Transform: func(text string, width int) string { transformed++; return text }}
	markdown := NewMarkdown(text, 0, 0, MarkdownTheme{}, nil, options)
	var work func() func()
	markdown.SetPreparation(&MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }, RequestRender: func() {}})
	initial := markdown.Render(80)
	if transformed != 0 || work == nil {
		t.Fatal("large message parsing ran on the owner")
	}
	if len(initial) == 0 {
		t.Fatal("cold preparation had no pending display")
	}
	apply := work()
	apply()
	want := NewMarkdown(text, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{}).Render(80)
	if got := markdown.Render(80); !reflect.DeepEqual(got, want) {
		t.Fatal("prepared render differs from synchronous rendering")
	}
}
