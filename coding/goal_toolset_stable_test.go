package coding

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// goalText executes a registered goal tool and returns its first text block.
func goalText(t *testing.T, session *AgentSession, name string, params string) string {
	t.Helper()
	tool, ok := session.control.Tools[name]
	if !ok {
		t.Fatalf("tool %s is not registered", name)
	}
	result, err := tool.Tool.Execute("id", json.RawMessage(params), context.Background(), func(agent.AgentToolResult) {})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	for _, block := range result.Content {
		if text, ok := block.(ai.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

// A provider caches the request prefix, and the tool array plus the system
// prompt's tools/rules sections sit at its front. Exposing a goal tool only
// while a goal is pursuing rewrites both, so every create, complete, pause and
// resume re-bills the whole conversation as uncached input (measured at 54k
// tokens for one create). The tool set therefore stays constant for the life of
// the session, and goal state reaches the model as appended messages only.
func TestGoalToolSetStaysConstantAcrossTransitions(t *testing.T) {
	dir := t.TempDir()
	manager := NewSessionManager(dir, nil)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Tools: []agent.AgentTool{testTool("read")},
		ConvertToLlm: ConvertToLlm, Goal: true, StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	// get_goal reports the absence of a goal rather than failing, so exposing it
	// for the whole session costs nothing but a little schema.
	if text := goalText(t, session, "get_goal", `{}`); !strings.Contains(text, `"goal": null`) {
		t.Fatalf("get_goal without a goal = %q", text)
	}
	toolsBefore := session.ActiveToolNames()
	promptBefore := session.SystemPrompt()
	if !reflect.DeepEqual(toolsBefore, []string{"read", "set_goal", "get_goal"}) {
		t.Fatalf("tools before any goal = %v", toolsBefore)
	}

	goalText(t, session, "set_goal", `{"objective":"keep the prefix stable"}`)
	if got := session.ActiveToolNames(); !reflect.DeepEqual(got, toolsBefore) {
		t.Fatalf("creating a goal changed the tool set: %v -> %v", toolsBefore, got)
	}
	if got := session.SystemPrompt(); got != promptBefore {
		t.Fatalf("creating a goal changed the system prompt (%d -> %d bytes)", len(promptBefore), len(got))
	}

	goalText(t, session, "set_goal", `{"status":"complete"}`)
	if got := session.ActiveToolNames(); !reflect.DeepEqual(got, toolsBefore) {
		t.Fatalf("completing a goal changed the tool set: %v -> %v", toolsBefore, got)
	}
	if got := session.SystemPrompt(); got != promptBefore {
		t.Fatalf("completing a goal changed the system prompt (%d -> %d bytes)", len(promptBefore), len(got))
	}
}
