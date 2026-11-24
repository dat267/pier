package coding

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// A displaced call wakes the model exactly once, with one notice carrying the
// result, queued as ordinary follow-up input so the transcript shape is the
// reference's.
func TestSessionWakesOncePerBackgroundCall(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools: []agent.AgentTool{testTool("read")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	handle := agent.ToolCallHandle{
		ID: "bg-1", ToolCallID: "c1", ToolName: "bash", OutputPath: "/tmp/out.log",
		Wait: func() agent.AgentToolResult {
			return agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "the slow output"}}}
		},
	}
	session.watchBackgroundCall(handle)

	deadline := time.Now().Add(5 * time.Second)
	var notices []ai.Message
	for time.Now().Before(deadline) {
		notices = session.Agent.TakeFollowUpMessages()
		if len(notices) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(notices) != 1 {
		t.Fatalf("wake-ups = %d, want exactly one", len(notices))
	}
	text := ""
	if user, ok := notices[0].(*ai.UserMessage); ok {
		if user.Content.Blocks == nil {
			text = user.Content.Text
		}
		for _, block := range user.Content.Blocks {
			if value, ok := block.(ai.TextContent); ok {
				text += value.Text
			}
		}
	}
	for _, want := range []string{"bg-1", "bash", "the slow output"} {
		if !strings.Contains(text, want) {
			t.Errorf("the notice lacks %q: %q", want, text)
		}
	}
	// No second notice: a backgrounded call reports once.
	time.Sleep(20 * time.Millisecond)
	if extra := session.Agent.TakeFollowUpMessages(); len(extra) != 0 {
		t.Fatalf("a second wake-up arrived: %d messages", len(extra))
	}
}

// A disposed session does not queue a notice: there is nobody to wake.
func TestSessionSkipsWakeAfterDispose(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools: []agent.AgentTool{testTool("read")},
	})
	if err != nil {
		t.Fatal(err)
	}
	agentRef := session.Agent
	handle := agent.ToolCallHandle{
		ID: "bg-2", ToolCallID: "c2", ToolName: "bash",
		Wait: func() agent.AgentToolResult {
			return agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "late"}}}
		},
	}
	session.Dispose()
	session.watchBackgroundCall(handle)
	time.Sleep(50 * time.Millisecond)
	if notices := agentRef.TakeFollowUpMessages(); len(notices) != 0 {
		t.Fatalf("a disposed session queued %d notices", len(notices))
	}
}
