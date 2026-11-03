package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// D358: a single thinking block is returned without the join copy, which
// otherwise made streaming thinking quadratic.
func BenchmarkAssistantThinking100KB(b *testing.B) {
	app, cleanup := newTestAppB(b)
	defer cleanup()
	streaming := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending}
	app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", streaming)})
	var body strings.Builder
	update := func() {
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageUpdate, Agent: agentEvent("message_update", &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending, Content: ai.ContentList{ai.ThinkingContent{Thinking: body.String()}}})})
		app.ui.RenderNow(true)
	}
	const total = 100 << 10
	delta := strings.Repeat("x", 16)
	b.ReportAllocs()
	b.SetBytes(total)
	for b.Loop() {
		for written := 0; written < total; written += len(delta) {
			body.WriteString(delta)
			update()
		}
	}
}
