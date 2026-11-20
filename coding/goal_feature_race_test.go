package coding

import (
	"sync"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Toggling the goal feature while the agent worker runs must not race: the
// controller field is written by SetGoalFeatureEnabled on the owner loop and
// read by the agent event path, the turn refresh and the settle emit. Before the
// guarded accessor the field was a plain pointer, which the race detector
// reports as soon as two goroutines touch it.
func TestGoalFeatureToggleRacesWithAgentEvents(t *testing.T) {
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

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			session.SetGoalFeatureEnabled(i%2 == 0)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			// The reads the agent worker performs on the controller.
			_ = session.GoalController()
			_ = session.goalHoldsFollowUps()
			_ = session.ToolNames()
			_ = session.GetActiveToolNames()
		}
	}()
	wg.Wait()
}
