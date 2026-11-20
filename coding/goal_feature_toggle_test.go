package coding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// The goal feature toggle (D496) turns the controller and its two tools on and
// off mid-session. Disabling it removes get_goal and set_goal from the active
// set and refuses /goal; enabling it registers and exposes them again. A goal
// already on the branch stays there, so re-enabling restores its state.
func TestGoalFeatureToggleEnablesAndDisablesTheController(t *testing.T) {
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
	hasTool := func(name string) bool {
		for _, active := range session.GetActiveToolNames() {
			if active == name {
				return true
			}
		}
		return false
	}
	if session.GoalController() == nil || !hasTool("set_goal") || !hasTool("get_goal") {
		t.Fatalf("goal feature off at construction: Goal=%v tools=%v", session.GoalController() != nil, session.GetActiveToolNames())
	}
	if _, err := session.control.Tools["set_goal"].Tool.Execute("create",
		json.RawMessage(`{"objective":"keep this objective"}`), context.Background(),
		func(agent.AgentToolResult) {}); err != nil {
		t.Fatal(err)
	}

	session.SetGoalFeatureEnabled(false)
	if session.GoalController() != nil {
		t.Fatal("controller still installed after disabling the goal feature")
	}
	if hasTool("set_goal") || hasTool("get_goal") {
		t.Fatalf("goal tools still active after disabling: %v", session.GetActiveToolNames())
	}
	if !hasTool("read") {
		t.Fatalf("disabling the goal feature dropped an unrelated tool: %v", session.GetActiveToolNames())
	}
	// A disabled feature has no controller for /goal to reach.
	if session.GoalController() != nil {
		t.Fatal("GoalController() still returns a controller after disabling")
	}

	session.SetGoalFeatureEnabled(true)
	if session.GoalController() == nil || !hasTool("set_goal") || !hasTool("get_goal") {
		t.Fatalf("goal feature off after enabling: Goal=%v tools=%v", session.GoalController() != nil, session.GetActiveToolNames())
	}
	// The goal stored on the branch comes back, disarmed, rather than vanishing.
	snapshot := session.GoalController().Snapshot()
	if snapshot.Goal == nil || snapshot.Goal.Objective != "keep this objective" {
		t.Fatalf("goal after re-enabling = %+v, want the stored objective", snapshot.Goal)
	}
}
