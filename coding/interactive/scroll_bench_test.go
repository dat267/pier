package interactive

import (
	"context"
	"fmt"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// BenchmarkScrollLongTranscript measures per-frame render cost on a long
// transcript (fullscreen viewport).
func BenchmarkScrollLongTranscript(b *testing.B) {
	app, cleanup := newTestAppB(b)
	defer cleanup()

	screen, _ := app.initialUI.(*tui.AltScreen)
	screen.Start()
	buildScrollTranscriptN(b, app, benchmarkMessageCount)

	for b.Loop() {
		app.ui.RenderNow(true)
	}
}

// BenchmarkScrollWarmDiff measures a warm incremental frame, which is what a
// keystroke triggers: no forced reset, so Container/Markdown caches are reused.
func BenchmarkScrollWarmDiff(b *testing.B) {
	app, cleanup := newTestAppB(b)
	defer cleanup()

	screen, _ := app.initialUI.(*tui.AltScreen)
	screen.Start()
	buildScrollTranscriptN(b, app, benchmarkMessageCount)
	app.ui.RenderNow(false) // warm every cache

	for b.Loop() {
		app.ui.RenderNow(false)
	}
}

// buildScrollTranscriptN appends n user/assistant message pairs.
func buildScrollTranscriptN(tb testing.TB, app *App, n int) {
	tb.Helper()
	for i := range n {
		app.events.HandleEvent(&coding.SessionEvent{
			Type: coding.SessionMessageStart,
			Agent: agentEvent("message_start", &ai.UserMessage{
				Content: ai.StringOrBlocks{Text: fmt.Sprintf("user message %d with some reasonably long text that wraps across lines in the terminal viewport", i)},
			}),
		})
		assistant := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
			Content: ai.ContentList{ai.TextContent{Text: fmt.Sprintf("# assistant reply %d\n\n```go\nfunc f%d() int { return %d }\n```\n\nsome paragraph with **bold** and `code` spans that wraps quite a bit as well to fill lines\n", i, i, i)}},
		}
		app.events.HandleEvent(&coding.SessionEvent{
			Type: coding.SessionMessageStart, Agent: agentEvent("message_start", assistant),
		})
		app.events.HandleEvent(&coding.SessionEvent{
			Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", assistant),
		})
	}
}

// newTestAppB is newTestApp on a 100x30 fullscreen terminal, initialized, for
// benchmarks and the tests that measure them.
func newTestAppB(tb testing.TB) (*App, func()) {
	tb.Helper()
	app, cleanup := newTestAppConfigured(tb, false, func(_ *coding.SettingsManager, options *AppOptions) {
		options.Terminal = &fakeRendererTerminal{width: 100, height: 30}
		options.TuiMode = "fullscreen"
	})
	app.Init(context.Background())
	return app, func() {
		app.Close()
		cleanup()
	}
}

var benchmarkMessageCount = 500
