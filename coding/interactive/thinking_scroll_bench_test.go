package interactive

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Benchmarks for the cost of expanded thinking blocks. buildThinkingTranscriptN
// appends n user/assistant pairs whose assistant message carries a thinking
// block of thinkingLines lines plus a short text block, so the thinking
// visibility toggle has something to expand.

func buildThinkingTranscriptN(tb testing.TB, app *App, n int, thinkingLines int) {
	tb.Helper()
	thinking := strings.Repeat("a line of reasoning that is long enough to wrap in a narrow viewport\n", thinkingLines)
	for i := range n {
		app.events.HandleEvent(&coding.SessionEvent{
			Type: coding.SessionMessageStart,
			Agent: agentEvent("message_start", &ai.UserMessage{
				Content: ai.StringOrBlocks{Text: fmt.Sprintf("user message %d", i)},
			}),
		})
		assistant := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
			Content: ai.ContentList{
				ai.ThinkingContent{Thinking: fmt.Sprintf("thinking for %d\n%s", i, thinking)},
				ai.TextContent{Text: fmt.Sprintf("reply %d", i)},
			},
		}
		app.events.HandleEvent(&coding.SessionEvent{
			Type: coding.SessionMessageStart, Agent: agentEvent("message_start", assistant),
		})
		app.events.HandleEvent(&coding.SessionEvent{
			Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", assistant),
		})
	}
}

func newThinkingScrollApp(tb testing.TB, hide bool, n int, thinkingLines int) (*App, func()) {
	app, cleanup := newTestAppB(tb)
	screen, _ := app.initialUI.(*tui.AltScreen)
	if screen != nil {
		screen.Start()
		screen.DisableAutoRender()
	}
	app.display.HideThinkingBlock = hide
	app.updateThinkingBlockVisibility(hide)
	buildThinkingTranscriptN(tb, app, n, thinkingLines)
	app.ui.RenderNow(true)
	app.ui.RenderNow(false)
	if app.transcriptScrollView == nil {
		cleanup()
		tb.Fatal("no transcript scroll view")
	}
	return app, cleanup
}

func benchmarkThinkingScrollBy(b *testing.B, hide bool, n int, thinkingLines int) {
	app, cleanup := newThinkingScrollApp(b, hide, n, thinkingLines)
	defer cleanup()
	scroll := app.transcriptScrollView
	scroll.ScrollToEnd()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scroll.ScrollBy(-1)
		if scroll.ScrollTop() == 0 {
			scroll.ScrollToEnd()
		}
		app.ui.RenderNow(false)
	}
}

// BenchmarkThinkingScrollHiddenScrollBy and BenchmarkThinkingScrollExpandedScrollBy
// measure a wheel-scroll frame (offset moves one line per frame) with every
// thinking block hidden or expanded.
func BenchmarkThinkingScrollHiddenScrollBy(b *testing.B) {
	benchmarkThinkingScrollBy(b, true, 500, 20)
}

func BenchmarkThinkingScrollExpandedScrollBy(b *testing.B) {
	benchmarkThinkingScrollBy(b, false, 500, 20)
}

// BenchmarkThinkingScrollExpandedScrollBy1500 is the scaling check: the frame
// cost must not grow with the transcript size.
func BenchmarkThinkingScrollExpandedScrollBy1500(b *testing.B) {
	benchmarkThinkingScrollBy(b, false, 1500, 20)
}

// BenchmarkThinkingExpandFirstFrame measures the ctrl+t frame that makes every
// thinking block visible for the first time (the markdown text is set then).
func BenchmarkThinkingExpandFirstFrame(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		app, cleanup := newThinkingScrollApp(b, true, 500, 20)
		b.StartTimer()
		app.updateThinkingBlockVisibility(false)
		app.ui.RenderNow(false)
		cleanup()
	}
}

func thinkingSessionItems(n int, thinkingLines int) []RenderSessionItem {
	thinking := strings.Repeat("a line of reasoning that is long enough to wrap in a narrow viewport\n", thinkingLines)
	items := make([]RenderSessionItem, 0, n*2)
	for i := range n {
		items = append(items, RenderSessionItem{Message: &ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("user message %d", i)}}})
		items = append(items, RenderSessionItem{Message: &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
			Content: ai.ContentList{
				ai.ThinkingContent{Thinking: fmt.Sprintf("thinking for %d\n%s", i, thinking)},
				ai.TextContent{Text: fmt.Sprintf("reply %d", i)},
			},
		}})
	}
	return items
}

// benchmarkMaterialize drains the lazily-deferred transcript with the
// synchronous warm path (PrerenderQueue nil): the worst case for the UI loop,
// and what a scroll-up over a resumed large session pays per chunk.
func benchmarkMaterialize(b *testing.B, hide bool, n int, thinkingLines int) {
	app, cleanup := newTestAppB(b)
	defer cleanup()
	screen, _ := app.initialUI.(*tui.AltScreen)
	if screen != nil {
		screen.Start()
		screen.DisableAutoRender()
	}
	app.display.HideThinkingBlock = hide
	app.updateThinkingBlockVisibility(hide)
	app.transcript.PrerenderQueue = nil
	items := thinkingSessionItems(n, thinkingLines)
	app.transcript.RenderSessionItems(items, false, true)
	app.ui.RenderNow(true)

	const width = 100
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		app.transcript.RenderSessionItems(items, false, true)
		b.StartTimer()
		for app.transcript.HasDeferred() {
			app.transcript.MaterializeDeferred(width)
			app.ui.RenderNow(false)
		}
	}
}

func BenchmarkThinkingMaterializeHidden(b *testing.B) {
	benchmarkMaterialize(b, true, 1000, 20)
}

func BenchmarkThinkingMaterializeExpanded(b *testing.B) {
	benchmarkMaterialize(b, false, 1000, 20)
}
