package interactive

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// D205: core/tools/renderers/bash.ts builds expanded Text synchronously.
func TestLargeBashResultPreparesOffLoop(t *testing.T) {
	InitTheme("dark", false)
	for _, name := range []string{"bash", "powershell"} {
		for _, expanded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/expanded=%t", name, expanded), func(t *testing.T) {
				c := NewToolExecutionComponent(name, "call", map[string]any{"command": "printf output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
				var work func() func()
				c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
				c.SetExpanded(expanded)
				result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("plain output\n", 6000)}}}
				c.UpdateResult(result, false)
				pending := c.Render(80)
				if work == nil {
					t.Fatal("large bash output ran inline")
				}
				apply := work()
				if !reflect.DeepEqual(c.Render(80), pending) {
					t.Fatal("worker changed attached display")
				}
				apply()
				sync := NewToolExecutionComponent(name, "call", map[string]any{"command": "printf output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
				sync.SetExpanded(expanded)
				sync.UpdateResult(result, false)
				if !reflect.DeepEqual(c.Render(80), sync.Render(80)) {
					t.Fatal("prepared bash differs from synchronous output")
				}
			})
		}
	}
}

// render-utils.ts:170-180 (pi v1.5.0 f9a26365) returns
// linkPath(theme.fg("accent", shortenPath(value)), value, cwd), and linkPath
// (:15-19) wraps that text in a pathToFileURL hyperlink when the terminal
// reports hyperlinks, resolving a relative path against cwd first. The port
// took cwd and ignored it, so a path rendered identically with hyperlinks on
// and off and was never clickable (D436).
func TestRenderToolPathLinksWhenHyperlinksAreOn(t *testing.T) {
	InitTheme("dark", false)
	previous := tui.GetTerminalCapabilities()
	t.Cleanup(func() { tui.SetTerminalCapabilities(previous) })
	theme := ActiveTheme()

	raw := "/home/dat/repos/pier/coding/paths.go"
	tui.SetTerminalCapabilities(tui.TerminalCapabilities{Hyperlinks: false})
	plain := RenderToolPath(&raw, theme, "/home/dat/repos/pier", "")
	if strings.Contains(plain, "\x1b]8;;") {
		t.Fatalf("hyperlinks off still emitted a link: %q", plain)
	}

	tui.SetTerminalCapabilities(tui.TerminalCapabilities{Hyperlinks: true})
	linked := RenderToolPath(&raw, theme, "/home/dat/repos/pier", "")
	if !strings.HasPrefix(linked, "\x1b]8;;file:///home/dat/repos/pier/coding/paths.go") {
		t.Errorf("path is not a file:// link: %q", linked)
	}
	if !strings.Contains(linked, plain) {
		t.Errorf("link dropped the styled path: %q does not contain %q", linked, plain)
	}

	// A relative path resolves against cwd before it becomes a URL.
	rel := "coding/paths.go"
	resolved := RenderToolPath(&rel, theme, "/home/dat/repos/pier", "")
	if !strings.Contains(resolved, "file:///home/dat/repos/pier/coding/paths.go") {
		t.Errorf("relative path not resolved against cwd: %q", resolved)
	}
}

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
