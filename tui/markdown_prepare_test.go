package tui

import (
	"reflect"
	"strings"
	"testing"
)

// largeMarkdownFixture is just over the 64 KiB detached-preparation threshold
// (largeMarkdownBytes). Keep it near the threshold: these tests assert threshold
// behavior, and a production-scale fixture only inflates the race-detector run.
func largeMarkdownFixture() string {
	return strings.Repeat("# Heading\n\nparagraph\n\n", 3000) // 66_000 bytes
}

// D204: requesting an already-cached width supersedes in-flight work too.
func TestMarkdownPreparationRejectsSupersededCachedWidth(t *testing.T) {
	text := largeMarkdownFixture()
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
	text := largeMarkdownFixture()
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
			text := largeMarkdownFixture()
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
	markdown.SetText(strings.Repeat("paragraph\n\n", 6000)) // 66_000 bytes
	if lines := markdown.Render(80); len(lines) != 0 {
		t.Fatal("preparation resurrected content from before an empty frame")
	}
}

// D204: a cold large Markdown must submit a detached immutable render, not
// execute parsing/transforms on the owner. Its completed frame is byte-identical.
func TestLargeMarkdownPreparationLeavesOwnerRenderingCheap(t *testing.T) {
	text := largeMarkdownFixture()
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

// D239: a streaming assistant message lives below the old 64 KiB threshold for
// its whole life, so every delta re-wrapped the growing open paragraph on the
// owner loop (the Oct 18 profile: 30 of 40 loop samples in
// markdown.Renderer.Finalize, one paint at 94ms). Prepared markdown diverts to
// the off-loop renderer from 8 KiB, and only from the second render: the first
// render stays synchronous, so history never shows a placeholder.
func TestStreamingMarkdownPreparesFromTheSecondRender(t *testing.T) {
	text := strings.Repeat("paragraph with enough words to wrap a little bit\n", 180) // ~8.6 KiB
	markdown := NewMarkdown(text, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
	submits := 0
	markdown.SetPreparation(&MarkdownPreparation{
		Submit: func(run func() func()) bool { submits++; run(); return true },
	})

	first := markdown.Render(80)
	if submits != 0 {
		t.Fatalf("the first render must stay synchronous (%d submits)", submits)
	}
	stale := append([]string(nil), first...)

	// The streaming delta: text grew, the cache misses, and the render must
	// divert to the preparation instead of re-wrapping on the owner.
	markdown.SetText(text + "and one more appended sentence for the delta.\n")
	second := markdown.Render(80)
	if submits != 1 {
		t.Fatalf("delta render submits = %d, want 1 (rendered on the owner)", submits)
	}
	if strings.Join(second, "\x00") != strings.Join(stale, "\x00") {
		t.Fatal("a delta render must paint the stale lines, not block on a fresh render")
	}
}

// D242: a streaming message below 8KiB still re-rendered its markdown on every
// coalesced paint (the 18:36 stall dump caught Markdown.Render -> Finalize ->
// WrapTextWithAnsi -> VisibleWidth on the loop). The off-loop threshold drops
// to 2KiB: history's first render stays synchronous, deltas then prepare off.
func TestStreamingMarkdownPreparesFrom2KiB(t *testing.T) {
	text := strings.Repeat("a short paragraph that wraps a little.\n", 80) // ~2.9 KiB
	if len(text) < 2*1024 {
		t.Fatalf("fixture is only %d bytes", len(text))
	}
	markdown := NewMarkdown(text, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
	submits := 0
	markdown.SetPreparation(&MarkdownPreparation{
		Submit: func(run func() func()) bool { submits++; return true },
	})
	markdown.Render(80) // first render stays synchronous
	markdown.SetText(text + "one more sentence for the delta.\n")
	markdown.Render(80)
	if submits != 1 {
		t.Fatalf("a ~3KiB delta render submits = %d, want 1", submits)
	}
}

// A streaming message can arrive in one large chunk, so its first render can
// already be over the threshold. That first render must also prepare off-loop
// (the live 18:59 stall: 106ms busy / 155ms keystroke while an assistant
// message rendered on the loop). Options.Streaming tells the markdown it is
// live, so the first render need not stay synchronous for a placeholder-free
// history.
func TestStreamingMarkdownPreparesOnTheFirstLargeRender(t *testing.T) {
	text := strings.Repeat("a short paragraph that wraps a little.\n", 80) // ~2.9 KiB
	markdown := NewMarkdown(text, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{Streaming: true})
	submits := 0
	markdown.SetPreparation(&MarkdownPreparation{
		Submit: func(run func() func()) bool { submits++; return true },
	})
	markdown.Render(80)
	if submits != 1 {
		t.Fatalf("first streaming render submits = %d, want 1", submits)
	}
}
