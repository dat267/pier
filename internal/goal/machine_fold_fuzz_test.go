package goal

import (
	"fmt"
	"testing"

	"github.com/dat267/pier/ai"
)

// A goal's live state is only as good as what a resume can rebuild from its
// entries: every admitted mutation persists a full snapshot, so folding the
// entries the machine appended must reproduce the machine's own goal. A
// transition that updates the view without persisting it (or persists a stale
// revision) diverges here, which is what a resumed session replays.
func FuzzMachineEntriesFoldBackToTheMachine(f *testing.F) {
	f.Add([]byte{0, 9, 7, 9, 1})
	f.Add([]byte{0, 10, 10, 10, 10})
	f.Add([]byte{4, 6, 2, 1, 7, 3, 5})
	f.Fuzz(func(t *testing.T, script []byte) {
		machine, clock := testMachine()
		var entries []CustomEntry
		for step, op := range script {
			clock.ms += 1000
			goal := machine.Snapshot().Goal
			result := machine.Dispatch(goalEventFor(op, step, clock.ms, goal))
			for _, effect := range result.Effects {
				appendEntry, ok := effect.(AppendEntryEffect)
				if !ok {
					continue
				}
				data, err := ai.MarshalJSON(appendEntry.Data)
				if err != nil {
					t.Fatalf("step %d: marshal %T: %v", step, appendEntry.Data, err)
				}
				entries = append(entries, CustomEntry{CustomType: appendEntry.EntryType, Data: data})
			}
			folded, err := FoldGoal(entries)
			if err != nil {
				t.Fatalf("step %d (%d entries): fold: %v", step, len(entries), err)
			}
			compareMachineAndFold(t, step, machine.Snapshot().Goal, folded.Goal)
		}
	})
}

// goalEventFor maps one script byte to a machine event. Events that name a goal
// use the live one, so the sequence reaches the transitions instead of only
// their refusals; a refusal is still a legal step and must fold the same way.
func goalEventFor(op byte, step int, now int64, goal *GoalView) GoalEvent {
	objective := fmt.Sprintf("objective-%d", step)
	id, revision := "", 0
	if goal != nil {
		id, revision = goal.ID, goal.Revision
	}
	switch op % 12 {
	case 0:
		return GoalCreateEvent{Objective: objective}
	case 1:
		return GoalResumeEvent{}
	case 2:
		return GoalPauseEvent{}
	case 3:
		return GoalClearEvent{ID: id, Revision: revision}
	case 4:
		return GoalSetEvent{Objective: objective}
	case 5:
		return GoalAgentSetEvent{Objective: objective}
	case 6:
		return GoalReplaceEvent{Objective: objective}
	case 7:
		return GoalUpdateEvent{GoalID: id, Revision: revision, Action: "complete"}
	case 8:
		return GoalUpdateEvent{GoalID: id, Revision: revision, Action: "blocked", BlockedReason: "blocked"}
	case 9:
		return AgentEndEvent{Usage: &GoalUsage{Tokens: 100, CostUsd: 0.01}}
	case 10:
		return AgentBeforeSettleEvent{Outcome: "stop", Now: now, HasNow: true}
	default:
		return AgentEndEvent{Aborted: true}
	}
}

// compareMachineAndFold asserts the durable fields match after each step.
func compareMachineAndFold(t *testing.T, step int, live, folded *GoalView) {
	t.Helper()
	if (live == nil) != (folded == nil) {
		t.Fatalf("step %d: live goal %v, folded %v", step, live, folded)
	}
	if live == nil {
		return
	}
	if live.ID != folded.ID || live.Revision != folded.Revision || live.Phase != folded.Phase ||
		live.Objective != folded.Objective || live.TurnsStarted != folded.TurnsStarted ||
		live.UsedTokens != folded.UsedTokens || live.UsedCostUsd != folded.UsedCostUsd {
		t.Fatalf("step %d: live %+v, folded %+v", step, *live, *folded)
	}
}
