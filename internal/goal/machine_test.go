package goal

import (
	"strings"
	"testing"
	"time"
)

// Mirrors ~/.pi/agent/extensions/goal/machine.test.ts.

func (c *fakeClock) now() time.Time { return time.UnixMilli(c.ms) }

func findContinue(result DispatchResult) *ContinueRoundEffect {
	for _, effect := range result.Effects {
		if round, ok := effect.(ContinueRoundEffect); ok {
			copied := round
			return &copied
		}
	}
	return nil
}

func TestAgentEndAdmitsTheCreatingRun(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "ship it"})
	result := m.Dispatch(AgentEndEvent{})
	if findAppend(result, GoalTurnType) == nil {
		t.Fatal("expected turn admission entry")
	}
	if m.Snapshot().Goal.TurnsStarted != 1 {
		t.Fatalf("turnsStarted=%d", m.Snapshot().Goal.TurnsStarted)
	}
}

func TestCompletedGoalAdmitsNoRounds(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "ship it"})
	m.Dispatch(GoalUpdateEvent{GoalID: m.Snapshot().Goal.ID, Revision: 1, Action: "complete"})
	result := m.Dispatch(AgentEndEvent{})
	if findAppend(result, GoalTurnType) != nil {
		t.Fatal("completed goal must not admit rounds")
	}
	if m.Snapshot().Goal.TurnsStarted != 0 {
		t.Fatalf("turnsStarted=%d", m.Snapshot().Goal.TurnsStarted)
	}
}

func TestPendingTurnAdmitsAndClearsReservation(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{makeChangeEntry(OpCreate, nil)}})
	m.Dispatch(GoalResumeEvent{})
	if m.Snapshot().PendingTurn == nil || *m.Snapshot().PendingTurn != 1 {
		t.Fatalf("pendingTurn=%v", m.Snapshot().PendingTurn)
	}
	result := m.Dispatch(AgentEndEvent{})
	if findAppend(result, GoalTurnType) == nil {
		t.Fatal("expected turn admission entry")
	}
	if m.Snapshot().PendingTurn != nil || m.Snapshot().Goal.TurnsStarted != 1 {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
}

func TestAbortedGoalAttemptPauses(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{makeChangeEntry(OpCreate, nil)}})
	m.Dispatch(GoalResumeEvent{})
	result := m.Dispatch(AgentEndEvent{Aborted: true})
	pause := findAppend(result, GoalCustomType)
	if pause == nil || pause.Data.(GoalChangeEntry).Operation != OpPause {
		t.Fatalf("expected pause entry: %+v", result.Effects)
	}
	if m.Snapshot().Goal.Phase != PhasePaused || m.Snapshot().Armed {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
	requirePausedMessage(t, result)
	notify := findNotify(result)
	if notify == nil || notify.Level != "warning" || !strings.Contains(notify.Message, "cancelled") {
		t.Fatalf("expected pause notify: %+v", result.Effects)
	}
}

func TestAbortedNonAttemptDisarmsOnly(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{makeChangeEntry(OpCreate, nil)}})
	result := m.Dispatch(AgentEndEvent{Aborted: true})
	if findAppend(result, "") != nil {
		t.Fatal("no entry expected")
	}
	if m.Snapshot().Armed {
		t.Fatal("expected disarmed")
	}
}

func TestBeforeSettleContinuesAnArmedGoal(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{makeChangeEntry(OpCreate, nil)}})
	m.Dispatch(GoalResumeEvent{})
	m.Dispatch(AgentEndEvent{})
	result := m.Dispatch(AgentBeforeSettleEvent{Outcome: "completed"})
	round := findContinue(result)
	if !result.Continue || round == nil || !strings.Contains(round.Content, "<goal_round>") {
		t.Fatalf("unexpected result: %+v", result)
	}
	if m.Snapshot().PendingTurn == nil || *m.Snapshot().PendingTurn != 2 {
		t.Fatalf("pendingTurn=%v", m.Snapshot().PendingTurn)
	}
}

func TestBeforeSettleProviderErrorPauses(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{makeChangeEntry(OpCreate, nil)}})
	m.Dispatch(GoalResumeEvent{})
	m.Dispatch(AgentEndEvent{})
	result := m.Dispatch(AgentBeforeSettleEvent{Outcome: "error"})
	if result.Continue {
		t.Fatal("an error must not continue")
	}
	if m.Snapshot().Goal.Phase != PhasePaused || m.Snapshot().Goal.BlockedReason.Code != "run-error" || m.Snapshot().PendingTurn != nil {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
	requirePausedMessage(t, result)
	notify := findNotify(result)
	if notify == nil || notify.Level != "warning" || !strings.Contains(notify.Message, "Goal paused") {
		t.Fatalf("expected pause notify: %+v", result.Effects)
	}
}

// A retryable failure does not pause the goal and does not reserve a round:
// the session's retry continues the same round, so re-reserving here would
// queue a duplicate round prompt per attempt.
func TestBeforeSettleRetryingKeepsTheGoalActive(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "ship it"})
	m.Dispatch(AgentEndEvent{})
	result := m.Dispatch(AgentBeforeSettleEvent{Outcome: "retrying"})
	if result.Continue || len(result.Effects) != 0 || result.Reply != nil {
		t.Fatalf("a retry must not continue or emit effects: %+v", result)
	}
	if m.Snapshot().Goal.Phase != PhaseActive || !m.Snapshot().Armed || m.Snapshot().PendingTurn != nil {
		t.Fatalf("a retry must leave the goal armed and active: %+v", m.Snapshot())
	}
}

func TestBeforeSettleAbortDurablyPauses(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "ship it"})
	result := m.Dispatch(AgentBeforeSettleEvent{Outcome: "aborted"})
	if result.Continue {
		t.Fatal("an abort must not continue")
	}
	if m.Snapshot().Goal.Phase != PhasePaused || m.Snapshot().Goal.BlockedReason.Code != "cancelled" {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
	requirePausedMessage(t, result)
	notify := findNotify(result)
	if notify == nil || notify.Level != "warning" || !strings.Contains(notify.Message, "cancelled") {
		t.Fatalf("expected pause notify: %+v", result.Effects)
	}
}

func TestDisarmedGoalsDoNotContinue(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{makeChangeEntry(OpCreate, nil)}})
	result := m.Dispatch(AgentBeforeSettleEvent{Outcome: "completed"})
	if result.Continue || findContinue(result) != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestBudgetTimePausesBeforeContinuation(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	result := m.Dispatch(GoalCreateEvent{Objective: "ship it", Budget: &GoalBudget{TimeLimitMs: new(int64(1000))}})
	_ = result
	now := m.Snapshot().Goal.CreatedAt + 1000
	settle := m.Dispatch(AgentBeforeSettleEvent{Outcome: "completed", Now: now, HasNow: true})
	if settle.Continue || m.Snapshot().Goal.BlockedReason.Code != "budget-time" {
		t.Fatalf("unexpected result: %+v snapshot=%+v", settle, m.Snapshot())
	}
	requirePausedMessage(t, settle)
}

func TestBudgetExhaustedCannotResume(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "ship it", Budget: &GoalBudget{TokenLimit: new(int64(10))}})
	m.Dispatch(AgentEndEvent{Usage: &GoalUsage{Tokens: 10}})
	m.Dispatch(AgentBeforeSettleEvent{Outcome: "completed"})
	resumed := m.Dispatch(GoalResumeEvent{})
	if !resumed.IsError || !strings.Contains(replyOf(resumed), "Token limit reached") {
		t.Fatalf("unexpected result: %+v", resumed)
	}
	if findSendMessage(resumed) != nil {
		t.Fatal("resume must not queue a round")
	}
}

func TestBudgetCostPausesBeforeContinuation(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "ship it", Budget: &GoalBudget{TimeLimitMs: new(int64(60_000)), CostLimitUsd: new(0.5)}})
	m.Dispatch(AgentEndEvent{Usage: &GoalUsage{Tokens: 20, CostUsd: 0.5}})
	result := m.Dispatch(AgentBeforeSettleEvent{Outcome: "completed"})
	if result.Continue || m.Snapshot().Goal.BlockedReason.Code != "budget-cost" {
		t.Fatalf("unexpected result: %+v snapshot=%+v", result, m.Snapshot())
	}
	requirePausedMessage(t, result)
}

// Budget exhaustion pauses even when the provider failure would be retried.
func TestBudgetTokensPausesAndKeepsUsage(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "ship it", Budget: &GoalBudget{TimeLimitMs: new(int64(60_000)), TokenLimit: new(int64(100))}})
	m.Dispatch(AgentEndEvent{Usage: &GoalUsage{Tokens: 100, CostUsd: 0.02}})
	result := m.Dispatch(AgentBeforeSettleEvent{Outcome: "retrying"})
	if result.Continue || m.Snapshot().Goal.Phase != PhasePaused || m.Snapshot().Goal.BlockedReason.Code != "budget-tokens" {
		t.Fatalf("unexpected result: %+v snapshot=%+v", result, m.Snapshot())
	}
	if m.Snapshot().Goal.UsedTokens != 100 || findAppend(result, "") == nil {
		t.Fatalf("unexpected usage/effects: %+v", result)
	}
	requirePausedMessage(t, result)
}

func TestBeforeSettlePendingMessagesSuppressContinuation(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "do it"})
	result := m.Dispatch(AgentBeforeSettleEvent{Outcome: "completed", HasPendingMessages: true})
	if result.Continue || m.Snapshot().PendingTurn != nil {
		t.Fatalf("unexpected result: %+v snapshot=%+v", result, m.Snapshot())
	}
}

func TestBeforeSettleWithoutPendingMessagesContinues(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "do it"})
	result := m.Dispatch(AgentBeforeSettleEvent{Outcome: "completed", HasPendingMessages: false})
	if !result.Continue || findContinue(result) == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
}
