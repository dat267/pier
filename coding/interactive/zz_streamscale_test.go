package interactive

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

func TestZZStreamScale(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	screen, _ := app.initialUI.(*tui.AltScreen)
	screen.Start()
	screen.DisableAutoRender()

	warm := 4000
	for i := 0; i < warm; i++ {
		assistant := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopStop,
			Content: ai.ContentList{ai.TextContent{Text: fmt.Sprintf("# reply %d\n\n```go\nfunc f%d() int { return %d }\n```\n\nsome paragraph with **bold** and `code` spans that wraps across the viewport width\n", i, i, i)}}}
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", assistant)})
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", assistant)})
	}
	var body strings.Builder
	for i := 0; i < 3000; i++ {
		body.WriteString(fmt.Sprintf("Paragraph %d with **bold**, `code` and a [link](https://example.com/%d) that wraps.\n\n", i, i))
	}
	streaming := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending}
	app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", streaming)})

	// First render of the big message (sync, per D239) — measure it.
	start := time.Now()
	app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageUpdate, Agent: agentEvent("message_update", &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending, Content: ai.ContentList{ai.TextContent{Text: body.String()}}})})
	app.ui.RenderNow(true)
	t.Logf("first big render: %s", time.Since(start))

	// Then 20 deltas like the live turn.
	start = time.Now()
	for i := 0; i < 20; i++ {
		body.WriteString("Another appended sentence with **bold** and a [link](https://example.com) that wraps across the width.\n\n")
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageUpdate, Agent: agentEvent("message_update", &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending, Content: ai.ContentList{ai.TextContent{Text: body.String()}}})})
		app.ui.RenderNow(true)
	}
	t.Logf("per delta: %s", time.Since(start)/20)
}
