package interactive

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// D205: live built-in tool results share D204's bounded optional queue.
func TestInputContinuesWhileLargeToolResultPreparationIsBlocked(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.transcript.Session = builtinRendererSession{}
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	app.markdownQueue.GoContext(func(context.Context) { <-release })
	stop := startLoopApp(t, app)
	defer stop()
	defer unblock()
	posted := make(chan struct{})
	app.ui.Post(func() {
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionStart, Agent: &agent.AgentEvent{Type: "tool_execution_start", ToolCallID: "t1", ToolName: "bash", Args: json.RawMessage(`{"command":"printf output"}`)}})
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionEnd, Agent: &agent.AgentEvent{Type: "tool_execution_end", ToolCallID: "t1", Result: agent.AgentToolResult{Content: ai.ContentList{ai.TextContent{Text: strings.Repeat("output\n", 10000)}}}}})
		lines := app.chat.Render(80)
		if !strings.Contains(strings.Join(lines, "\n"), "Preparing tool output...") {
			t.Error("live tool result rendered inline")
		}
		queued, running := app.markdownQueue.Backlog()
		if queued+running != 2 {
			t.Errorf("shared admission=%d, want blocker plus one result", queued+running)
		}
		close(posted)
	})
	select {
	case <-posted:
	case <-time.After(2 * time.Second):
		t.Fatal("tool result trapped owner")
	}
	app.postTerminalInput("responsive")
	waitForConditionWithin(t, func() bool { return strings.Contains(editorText(app), "responsive") }, 2*time.Second)
}
