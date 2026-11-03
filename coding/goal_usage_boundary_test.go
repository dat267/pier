package coding

import (
	"math"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
)

// D223's first-party goal contract accounts every assistant in an agent_end
// before checking the restored budget. Reference: pi v1.5.0 (f9a26365),
// packages/agent/src/agent-loop.ts and coding-agent/src/core/usage-totals.ts
// accumulate messages/usage without signed integer wraparound.
func TestGoalRoundUsageDoesNotOverflowBeforeBudgetCheck(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	state := goal.CreateGoalState("stay within the budget", time.UnixMilli(1), &goal.GoalBudget{TokenLimit: new(int64(500))})
	data, err := ai.MarshalJSON(goal.GoalChangeEntry{Operation: goal.OpCreate, Goal: &state, Timestamp: 1})
	if err != nil {
		t.Fatal(err)
	}
	sink.branchEntries = []goal.CustomEntry{{CustomType: goal.GoalCustomType, Data: data}}
	sink.streaming = true
	controller.Start("startup")
	controller.HandleCommand("resume")
	controller.AgentEnd(&agent.AgentEvent{Type: agent.AgentEnd, Messages: []ai.Message{
		&ai.AssistantMessage{StopReason: ai.StopToolUse, Usage: ai.Usage{TotalTokens: 6000000000000000000}},
		&ai.AssistantMessage{StopReason: ai.StopStop, Usage: ai.Usage{TotalTokens: 6000000000000000000}},
	}})

	view := controller.Snapshot().Goal
	if view == nil {
		t.Fatal("goal disappeared")
	}
	if view.UsedTokens != math.MaxInt64 {
		t.Errorf("round token usage = %d, want %d", view.UsedTokens, int64(math.MaxInt64))
	}
	if view.Phase != goal.PhasePaused || view.BlockedReason == nil || view.BlockedReason.Code != "budget-tokens" {
		t.Errorf("exhausted token budget did not pause the goal: %+v", view)
	}

	entries := append([]goal.CustomEntry(nil), sink.branchEntries...)
	for _, entry := range sink.emitted {
		entries = append(entries, goal.CustomEntry{CustomType: entry.CustomType, Data: entry.Data})
	}
	replayed, err := goal.FoldGoal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Goal == nil || replayed.Goal.UsedTokens != math.MaxInt64 || replayed.Goal.Phase != goal.PhasePaused {
		t.Fatalf("replay lost the exhausted usage/budget pause: %+v", replayed.Goal)
	}
}
