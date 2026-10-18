package interactive

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// allocatedBytesPerRunI is the coding package's allocatedBytesPerRun for this
// package: the delta path must not scale with transcript size.
func allocatedBytesPerRunI(t *testing.T, runs int, fn func()) int64 {
	t.Helper()
	fn()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < runs; i++ {
		fn()
	}
	runtime.ReadMemStats(&after)
	return int64(after.TotalAlloc-before.TotalAlloc) / int64(runs)
}

// A streaming delta's on-loop work must not depend on how long the transcript
// is. The stalls found in the wild were exactly this class of bug (GetSessionName
// copied every entry per paint; GetContextUsage re-walked the branch per paint),
// and this pins the delta path against it: 20 deltas at a small transcript and
// at a large one must allocate within a bounded ratio.
func TestStreamingDeltaCostIndependentOfTranscriptSize(t *testing.T) {
	run := func(warm int) int64 {
		app, cleanup := newTestAppB(t)
		defer cleanup()
		screen, _ := app.initialUI.(*tui.AltScreen)
		screen.Start()
		screen.DisableAutoRender()
		for i := 0; i < warm; i++ {
			a := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: fmt.Sprintf("reply %d with some words that wrap across the viewport\n", i)}}}
			app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", a)})
			app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", a)})
		}
		streaming := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending}
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", streaming)})
		body := ""
		return allocatedBytesPerRunI(t, 3, func() {
			for i := 0; i < 20; i++ {
				body += "appended sentence with **bold** and `code`.\n\n"
				app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageUpdate, Agent: agentEvent("message_update", &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending, Content: ai.ContentList{ai.TextContent{Text: body}}})})
				app.ui.RenderNow(true)
			}
		})
	}
	small := run(100)
	large := run(3000)
	t.Logf("delta allocations: %d bytes at 100 messages, %d at 3000", small, large)
	if large > small*4+256*1024 {
		t.Fatalf("streaming delta allocates %d bytes at 3000 messages vs %d at 100: the cost scales with transcript size", large, small)
	}
}
