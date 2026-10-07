package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

func TestTranscriptLargeToolResultPreparesOffLoop(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.Session = builtinRendererSession{}
	var work func() func()
	renderer.MarkdownPreparation = &tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }, RequestRender: func() { renderer.Chat.MarkDirty() }}
	assistant := &ai.AssistantMessage{Content: ai.ContentList{ai.ToolCall{ID: "t1", Name: "bash", Arguments: json.RawMessage(`{"command":"printf output"}`)}}, StopReason: ai.StopToolUse}
	result := &ai.ToolResultMessage{ToolCallID: "t1", ToolName: "bash", Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("output\n", 10000)}}}
	renderer.RenderSessionItems([]RenderSessionItem{{Message: assistant}, {Message: result}}, false, false)
	if lines := renderer.Chat.Render(80); work == nil || !strings.Contains(strings.Join(lines, "\n"), "Preparing tool output...") {
		t.Fatal("replayed tool result rendered inline")
	}
	work()()
	if strings.Contains(strings.Join(renderer.Chat.Render(80), "\n"), "Preparing tool output...") {
		t.Fatal("replay never applied complete output")
	}
}
