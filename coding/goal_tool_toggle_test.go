package coding

import (
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Disabling a goal tool from /tools must survive a goal transition: the goal
// feature syncs its two tools back on every transition, which would silently
// re-enable a tool the user turned off.
func TestDisabledGoalToolStaysDisabled(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm, Goal: true,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools: []agent.AgentTool{testTool("read")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	active := func() map[string]bool {
		out := map[string]bool{}
		for _, name := range session.GetActiveToolNames() {
			out[name] = true
		}
		return out
	}
	if !active()["set_goal"] {
		t.Fatal("set_goal not active at start")
	}
	if err := session.SetToolEnabled("set_goal", false); err != nil {
		t.Fatal(err)
	}
	if active()["set_goal"] {
		t.Fatal("set_goal still active right after disabling")
	}
	// A goal transition syncs the feature's tools.
	session.GoalController().Start("reload")
	if active()["set_goal"] {
		t.Fatal("a goal sync re-enabled a tool the user disabled")
	}
	if !active()["get_goal"] {
		t.Fatal("the untouched goal tool was dropped by the sync")
	}
}
