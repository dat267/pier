package goal

import (
	"math"
	"testing"
)

// D223's first-party goal machine records positive cumulative round usage.
// Stock pi v1.5.0 (f9a26365) has no goal machine. Its coding-agent/src/core/
// usage-totals.ts uses Numbers, so positive usage sums cannot wrap negative.
func TestGoalMachineDoesNotWrapCumulativeRoundUsage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		first  int64
		second int64
		want   int64
	}{
		{name: "huge", first: 6000000000000000000, second: 6000000000000000000, want: math.MaxInt64},
		{name: "ordinary", first: 120, second: 80, want: 200},
		{name: "negative ignored", first: -10, second: 80, want: 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machine, _ := testMachine()
			if result := machine.Dispatch(GoalCreateEvent{Objective: "account for each round"}); result.IsError {
				t.Fatal(replyOf(result))
			}
			machine.Dispatch(AgentEndEvent{Usage: &GoalUsage{Tokens: tc.first}})
			if result := machine.Dispatch(AgentBeforeSettleEvent{Outcome: "completed"}); !result.Continue {
				t.Fatal("next round was not reserved")
			}
			machine.Dispatch(AgentEndEvent{Usage: &GoalUsage{Tokens: tc.second}})
			view := machine.Snapshot().Goal
			if view.UsedTokens != tc.want {
				t.Fatalf("cumulative tokens = %d, want %d", view.UsedTokens, tc.want)
			}
		})
	}
}

// D223 replay must restore the same positive usage as live rounds. Reference:
// pi v1.5.0 (f9a26365) coding-agent/src/core/usage-totals.ts uses Number sums.
func TestFoldGoalDoesNotWrapCumulativeRoundUsage(t *testing.T) {
	state := createTestGoal("restore every round's usage", t0, nil)
	folded, err := FoldGoal([]CustomEntry{
		changeEntry(OpCreate, &state, t0, nil),
		turnEntry(state.ID, 1, 1, t0+1, &GoalUsage{Tokens: 6000000000000000000}),
		turnEntry(state.ID, 1, 2, t0+2, &GoalUsage{Tokens: 6000000000000000000}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Goal == nil {
		t.Fatal("goal disappeared during replay")
	}
	if folded.Goal.UsedTokens != math.MaxInt64 {
		t.Fatalf("replayed cumulative tokens = %d, want %d", folded.Goal.UsedTokens, int64(math.MaxInt64))
	}
}
