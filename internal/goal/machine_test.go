package goal

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Mirrors ~/.pi/agent/extensions/goal/machine.test.ts.

type fakeClock struct{ ms int64 }

func (c *fakeClock) now() time.Time { return time.UnixMilli(c.ms) }

func testMachine() (*GoalMachine, *fakeClock) {
	clock := &fakeClock{ms: t0}
	machine := NewGoalMachine()
	machine.SetClock(clock.now)
	return machine, clock
}

func makeChangeEntry(operation GoalOperation, goal *GoalSnapshot) CustomEntry {
	if goal == nil {
		g := createTestGoal("test", t0, nil)
		goal = &g
	}
	return changeEntry(operation, goal, t0, nil)
}

func makeTurnEntry(turn int, goal *GoalSnapshot) CustomEntry {
	return turnEntry(goal.ID, goal.Revision, turn, t0, nil)
}

func replyOf(result DispatchResult) string {
	if result.Reply != nil {
		return *result.Reply
	}
	return ""
}

func findAppend(result DispatchResult, entryType string) *AppendEntryEffect {
	for _, effect := range result.Effects {
		if appendEntry, ok := effect.(AppendEntryEffect); ok && (entryType == "" || appendEntry.EntryType == entryType) {
			copied := appendEntry
			return &copied
		}
	}
	return nil
}

func findSendMessage(result DispatchResult) *SendMessageEffect {
	for _, effect := range result.Effects {
		if message, ok := effect.(SendMessageEffect); ok {
			copied := message
			return &copied
		}
	}
	return nil
}

// requirePausedMessage asserts the transition emitted the model-facing pause
// notice: a context-only custom message of kind "paused".
func requirePausedMessage(t *testing.T, result DispatchResult) *SendMessageEffect {
	t.Helper()
	message := findSendMessage(result)
	if message == nil {
		t.Fatalf("expected a paused message: %+v", result.Effects)
	}
	if message.CustomType != GoalEventType || message.Details["kind"] != "paused" {
		t.Fatalf("unexpected paused message: %+v", message)
	}
	if message.Display || message.TriggerTurn {
		t.Fatalf("paused message must be context-only: %+v", message)
	}
	if !strings.Contains(message.Content, "<goal_paused>") {
		t.Fatalf("paused message must frame the stop: %q", message.Content)
	}
	return message
}

func findNotify(result DispatchResult) *NotifyEffect {
	for _, effect := range result.Effects {
		if notify, ok := effect.(NotifyEffect); ok {
			copied := notify
			return &copied
		}
	}
	return nil
}

func findContinue(result DispatchResult) *ContinueRoundEffect {
	for _, effect := range result.Effects {
		if round, ok := effect.(ContinueRoundEffect); ok {
			copied := round
			return &copied
		}
	}
	return nil
}

func TestSessionStartIgnoresUnrelatedEntriesWithoutAllocating(t *testing.T) {
	// Port of goal/machine.ts session_start: unrelated custom entries do not
	// participate in replay, so a large branch containing only other extensions
	// must not allocate a filtered copy before the fold.
	entries := make([]CustomEntry, 1024)
	for index := range entries {
		entries[index] = CustomEntry{CustomType: "other-extension"}
	}
	machine := NewGoalMachine()
	allocations := testing.AllocsPerRun(10, func() {
		machine.Dispatch(SessionStartEvent{Entries: entries, Reason: "startup"})
		if machine.Snapshot().Goal != nil {
			t.Fatal("session start created a goal")
		}
	})
	if allocations != 0 {
		t.Fatalf("unrelated entries allocated %.0f objects per replay, want 0", allocations)
	}
}

func TestSessionStartNoEntries(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: nil})
	if m.Snapshot().Goal != nil || m.Snapshot().Armed {
		t.Fatal("expected empty disarmed state")
	}
}

func TestSessionStartRestoresActiveDisarmed(t *testing.T) {
	m, _ := testMachine()
	entry := makeChangeEntry(OpCreate, nil)
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{entry}})
	if m.Snapshot().Goal == nil || m.Snapshot().Goal.Objective != "test" || m.Snapshot().Goal.Phase != PhaseActive {
		t.Fatalf("unexpected goal: %+v", m.Snapshot().Goal)
	}
	if m.Snapshot().Armed {
		t.Fatal("restored goal must be disarmed")
	}
}

func TestSessionStartRestoresTurnsFromFold(t *testing.T) {
	g := createTestGoal("test", t0, nil)
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		makeChangeEntry(OpCreate, &g),
		makeTurnEntry(1, &g),
		makeTurnEntry(2, &g),
	}})
	if m.Snapshot().Goal.TurnsStarted != 2 {
		t.Fatalf("turnsStarted=%d", m.Snapshot().Goal.TurnsStarted)
	}
}

func TestGoalCreateAppendsEntryAndArms(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	result := m.Dispatch(GoalCreateEvent{Objective: "ship it"})
	entry := findAppend(result, GoalCustomType)
	if entry == nil {
		t.Fatal("expected appendEntry effect")
	}
	if entry.Data.(GoalChangeEntry).Operation != OpCreate {
		t.Fatalf("unexpected entry: %+v", entry.Data)
	}
	if !m.Snapshot().Armed || m.Snapshot().Goal.Objective != "ship it" {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
}

func TestGoalCreateOverUnfinishedGoalFails(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{makeChangeEntry(OpCreate, nil)}})
	result := m.Dispatch(GoalCreateEvent{Objective: "another"})
	if !result.IsError || !strings.Contains(replyOf(result), "already exists") {
		t.Fatalf("unexpected result: %+v", result)
	}
	if findAppend(result, "") != nil {
		t.Fatal("no mutation expected")
	}
	if m.Snapshot().Goal.Objective != "test" {
		t.Fatalf("goal changed: %+v", m.Snapshot().Goal)
	}
}

func TestGoalCreateOverCompletedGoalIsAllowed(t *testing.T) {
	past := t0 - 10_000
	g := createTestGoal("old", past, nil)
	done := g
	done.Phase, done.Revision, done.UpdatedAt = PhaseComplete, 2, past+1
	m, clock := testMachine()
	clock.ms = past + 2
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		changeEntry(OpCreate, &g, past, nil),
		changeEntry(OpComplete, &done, past+1, nil),
	}})
	m.Dispatch(GoalCreateEvent{Objective: "fresh"})
	if m.Snapshot().Goal.Objective != "fresh" {
		t.Fatalf("unexpected goal: %+v", m.Snapshot().Goal)
	}
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

func armedMachine(t *testing.T) *GoalMachine {
	t.Helper()
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "ship it"})
	return m
}

func TestGoalUpdateCompleteAppendsAndWrapsUp(t *testing.T) {
	m := armedMachine(t)
	ref := m.Snapshot().Goal
	result := m.Dispatch(GoalUpdateEvent{GoalID: ref.ID, Revision: ref.Revision, Action: "complete"})
	if result.IsError || !strings.Contains(replyOf(result), "complete") {
		t.Fatalf("unexpected result: %+v", result)
	}
	entry := findAppend(result, GoalCustomType)
	if entry == nil || entry.Data.(GoalChangeEntry).Operation != OpComplete {
		t.Fatalf("expected complete entry: %+v", result.Effects)
	}
	if findSendMessage(result) == nil {
		t.Fatal("expected wrapup message")
	}
	if m.Snapshot().Goal.Phase != PhaseComplete || m.Snapshot().Armed {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
}

// The agent may clear its own goal (port-local, D232): clearing tombstones it
// so a new goal can be created, and it stops the round loop.
func TestGoalUpdateClearTombstonesTheGoal(t *testing.T) {
	m := armedMachine(t)
	ref := m.Snapshot().Goal
	result := m.Dispatch(GoalUpdateEvent{GoalID: ref.ID, Revision: ref.Revision, Action: "clear"})
	if result.IsError {
		t.Fatalf("unexpected error: %+v", result)
	}
	entry := findAppend(result, GoalCustomType)
	if entry == nil || entry.Data.(GoalChangeEntry).Operation != OpClear {
		t.Fatalf("expected clear entry: %+v", result.Effects)
	}
	if m.Snapshot().Goal != nil {
		t.Fatalf("goal still present: %+v", m.Snapshot().Goal)
	}
	if m.Snapshot().Armed {
		t.Fatal("cleared goal must be disarmed")
	}
	if findSendMessage(result) != nil {
		t.Fatal("clear must not queue a round or wrap-up")
	}
}

func TestGoalUpdateClearStaleRefFails(t *testing.T) {
	m := armedMachine(t)
	ref := m.Snapshot().Goal
	result := m.Dispatch(GoalUpdateEvent{GoalID: ref.ID, Revision: 999, Action: "clear"})
	if !result.IsError || m.Snapshot().Goal == nil {
		t.Fatalf("stale clear must not clear: %+v", result)
	}
}

// The agent's set_goal objective path replaces a live goal without queueing a
// round (the run is active; agent_before_settle reserves the next round).
func TestGoalAgentSetReplacesWithoutQueueingARound(t *testing.T) {
	m := armedMachine(t)
	previous := m.Snapshot().Goal
	result := m.Dispatch(GoalAgentSetEvent{Objective: "second"})
	if result.IsError {
		t.Fatalf("unexpected error: %+v", result)
	}
	if findSendMessage(result) != nil {
		t.Fatal("agent set must not queue a round")
	}
	ops := []GoalOperation{}
	for _, effect := range result.Effects {
		if entry, ok := effect.(AppendEntryEffect); ok {
			ops = append(ops, entry.Data.(GoalChangeEntry).Operation)
		}
	}
	if len(ops) != 2 || ops[0] != OpClear || ops[1] != OpCreate {
		t.Fatalf("ops = %v", ops)
	}
	goal := m.Snapshot().Goal
	if goal == nil || goal.Objective != "second" || goal.ID == previous.ID || !m.Snapshot().Armed {
		t.Fatalf("goal = %+v", goal)
	}
}

// With no live goal, the agent's set_goal is a plain create with no round.
func TestGoalAgentSetCreatesWithoutQueueingARound(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	result := m.Dispatch(GoalAgentSetEvent{Objective: "ship it"})
	entry := findAppend(result, GoalCustomType)
	if entry == nil || entry.Data.(GoalChangeEntry).Operation != OpCreate {
		t.Fatalf("expected create entry: %+v", result.Effects)
	}
	if findSendMessage(result) != nil {
		t.Fatal("agent set must not queue a round")
	}
}

func TestGoalUpdateStaleRefFails(t *testing.T) {
	m := armedMachine(t)
	real := m.Snapshot().Goal
	result := m.Dispatch(GoalUpdateEvent{GoalID: real.ID, Revision: 999, Action: "complete"})
	if !result.IsError || !strings.Contains(replyOf(result), "revision 999") || findAppend(result, "") != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if m.Snapshot().Goal.Phase != PhaseActive {
		t.Fatal("goal must be untouched")
	}
}

func TestGoalUpdateUnknownIDNamesCurrent(t *testing.T) {
	m := armedMachine(t)
	result := m.Dispatch(GoalUpdateEvent{GoalID: "goal_9f5b6c48e33d", Revision: 1, Action: "complete"})
	if !result.IsError || !strings.Contains(replyOf(result), m.Snapshot().Goal.ID) || !strings.Contains(replyOf(result), "rev 1") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGoalUpdateRejectsUnknownAction(t *testing.T) {
	m := armedMachine(t)
	result := m.Dispatch(GoalUpdateEvent{GoalID: "any", Revision: 1, Action: "pause"})
	if !result.IsError || !strings.Contains(replyOf(result), `Unknown action "pause"`) || !strings.Contains(replyOf(result), "complete") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGoalUpdateRejectsNonNumericRevision(t *testing.T) {
	m := armedMachine(t)
	for _, bad := range []any{nil, "abc"} {
		result := m.Dispatch(GoalUpdateEvent{GoalID: "any", Revision: bad, Action: "complete"})
		if !result.IsError || !strings.Contains(replyOf(result), "revision") {
			t.Fatalf("revision %v must be rejected: %+v", bad, result)
		}
	}
}

func TestGoalCreateReplyCarriesIDAndRevision(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	result := m.Dispatch(GoalCreateEvent{Objective: "ship it"})
	if !strings.Contains(replyOf(result), m.Snapshot().Goal.ID) || !strings.Contains(replyOf(result), "revision 1") {
		t.Fatalf("unexpected reply: %q", replyOf(result))
	}
}

func TestGoalUpdateBlockedBeforeThreeRounds(t *testing.T) {
	m := armedMachine(t)
	ref := m.Snapshot().Goal
	result := m.Dispatch(GoalUpdateEvent{GoalID: ref.ID, Revision: ref.Revision, Action: "blocked", BlockedReason: "stuck"})
	if !result.IsError || !strings.Contains(replyOf(result), "3 consecutive") || m.Snapshot().Goal.Phase != PhaseActive {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGoalUpdateBlockedAfterThreeRounds(t *testing.T) {
	m := armedMachine(t)
	for range 3 {
		m.Dispatch(AgentEndEvent{})
		m.Dispatch(AgentBeforeSettleEvent{Outcome: "completed"})
	}
	m.Dispatch(AgentEndEvent{})
	ref := m.Snapshot().Goal
	result := m.Dispatch(GoalUpdateEvent{GoalID: ref.ID, Revision: ref.Revision, Action: "blocked", BlockedReason: "stuck"})
	if result.IsError || !strings.Contains(replyOf(result), "blocked") {
		t.Fatalf("unexpected result: %+v", result)
	}
	if entry := findAppend(result, GoalCustomType); entry == nil || entry.Data.(GoalChangeEntry).Operation != OpBlock {
		t.Fatalf("expected block entry: %+v", result.Effects)
	}
	if m.Snapshot().Goal.Phase != PhaseBlocked {
		t.Fatal("expected blocked phase")
	}
}

func TestGoalPauseCommand(t *testing.T) {
	m := armedMachine(t)
	result := m.Dispatch(GoalPauseEvent{})
	if entry := findAppend(result, GoalCustomType); entry == nil || entry.Data.(GoalChangeEntry).Operation != OpPause {
		t.Fatalf("expected pause entry: %+v", result.Effects)
	}
	if m.Snapshot().Goal.Phase != PhasePaused || m.Snapshot().Armed {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
	requirePausedMessage(t, result)
}

// D239: a pause sends the model the stop reason and the objective as reference
// only, so the paused goal is not silently pursued after it stops.
func TestPausedMessageCarriesTheStopReason(t *testing.T) {
	m := armedMachine(t)
	message := requirePausedMessage(t, m.Dispatch(GoalPauseEvent{}))
	for _, want := range []string{"human-paused", "Paused by user.", "ship it", "do not continue", "/goal resume"} {
		if !strings.Contains(message.Content, want) {
			t.Fatalf("paused message missing %q: %q", want, message.Content)
		}
	}
}

func TestGoalClearCommand(t *testing.T) {
	m := armedMachine(t)
	ref := m.Snapshot().Goal
	result := m.Dispatch(GoalClearEvent{ID: ref.ID, Revision: ref.Revision})
	if entry := findAppend(result, GoalCustomType); entry == nil || entry.Data.(GoalChangeEntry).Operation != OpClear {
		t.Fatalf("expected clear entry: %+v", result.Effects)
	}
	if m.Snapshot().Goal != nil {
		t.Fatal("expected nil goal")
	}
}

func TestMachineDoesNotExposeRemovedBannerState(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		makeChangeEntry(OpCreate, nil),
		{CustomType: "pi-goal-settings", Data: json.RawMessage(`{"bannerEnabled":true}`)},
	}})
	if m.Snapshot().Goal == nil || m.Snapshot().Goal.Phase != PhaseActive {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
}

func TestCommitRoutesThroughApplyChange(t *testing.T) {
	m := armedMachine(t)
	goal := m.Snapshot().Goal
	m.Dispatch(GoalUpdateEvent{GoalID: goal.ID, Revision: goal.Revision, Action: "complete"})
	completed := m.Snapshot().Goal
	illegal := completed.GoalSnapshot
	illegal.Phase = PhasePaused
	illegal.Revision = completed.Revision + 1
	illegal.BlockedReason = &BlockedReason{Code: "x", Message: "y"}
	if _, err := m.commit(OpPause, &illegal, nil); err == nil || !strings.Contains(err.Error(), "illegal transition") {
		t.Fatalf("expected illegal transition error, got %v", err)
	}
	if m.Snapshot().Goal.Phase != PhaseComplete || m.Snapshot().Goal.Revision != completed.Revision {
		t.Fatal("rejected mutation must leave the goal untouched")
	}
}

func TestGoalSetCreatesAndQueuesRound(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	result := m.Dispatch(GoalSetEvent{Objective: "from command"})
	if entry := findAppend(result, GoalCustomType); entry == nil || entry.Data.(GoalChangeEntry).Operation != OpCreate {
		t.Fatalf("expected create entry: %+v", result.Effects)
	}
	if findSendMessage(result) == nil {
		t.Fatal("expected immediate round")
	}
	if m.Snapshot().PendingTurn == nil || *m.Snapshot().PendingTurn != 1 || m.Snapshot().Goal.Objective != "from command" {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
}

func TestSessionStartCorruptionNotifies(t *testing.T) {
	g := createTestGoal("test", t0, nil)
	m, _ := testMachine()
	corrupt := g
	corrupt.Revision, corrupt.UpdatedAt = 5, t0+1
	result := m.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		makeChangeEntry(OpCreate, &g),
		changeEntry(OpResume, &corrupt, t0+1, nil),
	}})
	if m.Snapshot().Goal != nil {
		t.Fatal("corrupt state must clear the goal")
	}
	notify := findNotify(result)
	if notify == nil || notify.Level != "warning" || !strings.Contains(notify.Message, "corrupt") {
		t.Fatalf("expected corruption notify: %+v", result.Effects)
	}
	if result.Reply != nil {
		t.Fatal("corruption must not reply")
	}
}

type recordingMachine struct {
	machine *GoalMachine
	entries []CustomEntry
}

func machineWithCollector() *recordingMachine {
	machine, _ := testMachine()
	recorder := &recordingMachine{machine: machine}
	return recorder
}

func (r *recordingMachine) dispatch(event GoalEvent) DispatchResult {
	result := r.machine.Dispatch(event)
	for _, effect := range result.Effects {
		if entry, ok := effect.(AppendEntryEffect); ok {
			data, _ := json.Marshal(entry.Data)
			r.entries = append(r.entries, CustomEntry{CustomType: entry.EntryType, Data: data})
		}
	}
	return result
}

func TestRoundTripSetOverComplete(t *testing.T) {
	recorder := machineWithCollector()
	m := recorder.machine
	recorder.dispatch(SessionStartEvent{})
	recorder.dispatch(GoalCreateEvent{Objective: "first"})
	first := m.Snapshot().Goal
	recorder.dispatch(GoalUpdateEvent{GoalID: first.ID, Revision: first.Revision, Action: "complete"})
	recorder.dispatch(GoalSetEvent{Objective: "second"})
	second := m.Snapshot().Goal

	fresh, _ := testMachine()
	fresh.Dispatch(SessionStartEvent{Entries: recorder.entries})
	if fresh.Snapshot().Goal == nil || fresh.Snapshot().Goal.ID != second.ID || fresh.Snapshot().Goal.Objective != "second" || fresh.Snapshot().Goal.Phase != PhaseActive {
		t.Fatalf("unexpected replayed goal: %+v", fresh.Snapshot().Goal)
	}
}

func TestRoundTripFullLifecycle(t *testing.T) {
	recorder := machineWithCollector()
	m := recorder.machine
	recorder.dispatch(SessionStartEvent{})
	recorder.dispatch(GoalCreateEvent{Objective: "round trip"})
	recorder.dispatch(AgentEndEvent{})
	recorder.dispatch(AgentBeforeSettleEvent{Outcome: "completed"})
	recorder.dispatch(AgentEndEvent{})
	recorder.dispatch(AgentBeforeSettleEvent{Outcome: "completed"})
	recorder.dispatch(AgentEndEvent{})
	recorder.dispatch(GoalPauseEvent{})
	recorder.dispatch(GoalResumeEvent{})
	recorder.dispatch(AgentEndEvent{})
	snapshot := m.Snapshot().Goal
	recorder.dispatch(GoalUpdateEvent{GoalID: snapshot.ID, Revision: snapshot.Revision, Action: "complete"})
	final := m.Snapshot().Goal

	fresh, _ := testMachine()
	fresh.Dispatch(SessionStartEvent{Entries: recorder.entries})
	replayed := fresh.Snapshot().Goal
	if replayed == nil || replayed.ID != final.ID || replayed.Revision != final.Revision || replayed.Objective != final.Objective || replayed.Phase != PhaseComplete || replayed.TurnsStarted != final.TurnsStarted {
		t.Fatalf("replayed %+v, want %+v", replayed, final)
	}
}

func TestRoundTripMidLifecyclePause(t *testing.T) {
	recorder := machineWithCollector()
	recorder.dispatch(SessionStartEvent{})
	recorder.dispatch(GoalCreateEvent{Objective: "pause me"})
	recorder.dispatch(GoalPauseEvent{})

	fresh, _ := testMachine()
	fresh.Dispatch(SessionStartEvent{Entries: recorder.entries})
	g := fresh.Snapshot().Goal
	if g == nil || g.Phase != PhasePaused || g.BlockedReason.Code != "human-paused" || g.TurnsStarted != 0 {
		t.Fatalf("unexpected replayed goal: %+v", g)
	}
}

func TestSessionStartIgnoresNonGoalCustomEntries(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		{CustomType: "pi-todo", Data: json.RawMessage(`{"noise":true}`)},
		makeChangeEntry(OpCreate, nil),
		{CustomType: GoalEventType, Data: json.RawMessage(`{"noise":true}`)},
	}})
	if m.Snapshot().Goal == nil || m.Snapshot().Goal.Objective != "test" {
		t.Fatalf("unexpected goal: %+v", m.Snapshot().Goal)
	}
}

func TestReloadPausesAnActiveGoalDurably(t *testing.T) {
	m, _ := testMachine()
	result := m.Dispatch(SessionStartEvent{Entries: []CustomEntry{makeChangeEntry(OpCreate, nil)}, Reason: "reload"})
	entry := findAppend(result, GoalCustomType)
	if entry == nil || entry.Data.(GoalChangeEntry).Operation != OpPause || entry.Data.(GoalChangeEntry).Goal.BlockedReason.Code != "reloaded" {
		t.Fatalf("expected reload pause: %+v", result.Effects)
	}
	if m.Snapshot().Goal.Phase != PhasePaused || m.Snapshot().Armed {
		t.Fatalf("unexpected snapshot: %+v", m.Snapshot())
	}
	// D239: the pause notice is context-only; it must not start a run.
	requirePausedMessage(t, result)
	notify := findNotify(result)
	if notify == nil || !strings.Contains(notify.Message, "reload") || !strings.Contains(notify.Message, "/goal resume") {
		t.Fatalf("expected reload notify: %+v", result.Effects)
	}
}

func TestReloadClockSkewKeepsTheGoal(t *testing.T) {
	future := t0 + 60_000
	g := createTestGoal("test", future, nil)
	m, _ := testMachine()
	result := m.Dispatch(SessionStartEvent{Entries: []CustomEntry{changeEntry(OpCreate, &g, t0, nil)}, Reason: "reload"})
	if m.Snapshot().Goal == nil || m.Snapshot().Goal.ID != g.ID || m.Snapshot().Goal.Phase != PhaseActive {
		t.Fatalf("goal must survive a failed pause: %+v", m.Snapshot().Goal)
	}
	notify := findNotify(result)
	if notify == nil || notify.Level != "warning" || !strings.Contains(notify.Message, "not paused") {
		t.Fatalf("expected warning notify: %+v", result.Effects)
	}
}

func TestReloadLeavesAnAlreadyPausedGoalAlone(t *testing.T) {
	g := createTestGoal("test", t0, nil)
	paused := g
	paused.Phase, paused.Revision, paused.UpdatedAt = PhasePaused, 2, t0+1
	paused.BlockedReason = &BlockedReason{Code: "human-paused", Message: "Paused by user."}
	m, _ := testMachine()
	result := m.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		changeEntry(OpCreate, &g, t0, nil),
		changeEntry(OpPause, &paused, t0+1, nil),
	}, Reason: "reload"})
	if findAppend(result, "") != nil {
		t.Fatal("already-paused goal must not be re-paused")
	}
	if m.Snapshot().Goal.Phase != PhasePaused {
		t.Fatalf("unexpected phase: %s", m.Snapshot().Goal.Phase)
	}
}

func TestStartupAndResumeKeepThePhaseActive(t *testing.T) {
	for _, reason := range []string{"startup", "resume", "new", "fork"} {
		m, _ := testMachine()
		result := m.Dispatch(SessionStartEvent{Entries: []CustomEntry{makeChangeEntry(OpCreate, nil)}, Reason: reason})
		if findAppend(result, "") != nil {
			t.Fatalf("%s writes nothing", reason)
		}
		if m.Snapshot().Goal.Phase != PhaseActive || m.Snapshot().Armed {
			t.Fatalf("%s: unexpected snapshot: %+v", reason, m.Snapshot())
		}
	}
}

func TestGoalReplaceTombstonesAndCreates(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "first"})
	before := m.Snapshot().Goal
	result := m.Dispatch(GoalReplaceEvent{Objective: "second"})
	entries := []AppendEntryEffect{}
	for _, effect := range result.Effects {
		if entry, ok := effect.(AppendEntryEffect); ok && entry.EntryType == GoalCustomType {
			entries = append(entries, entry)
		}
	}
	if len(entries) != 2 {
		t.Fatalf("expected clear then create: %+v", result.Effects)
	}
	if entries[0].Data.(GoalChangeEntry).Operation != OpClear || entries[0].Data.(GoalChangeEntry).Cleared.ID != before.ID {
		t.Fatalf("unexpected clear: %+v", entries[0].Data)
	}
	if entries[1].Data.(GoalChangeEntry).Operation != OpCreate {
		t.Fatalf("unexpected create: %+v", entries[1].Data)
	}
	if m.Snapshot().Goal.Objective != "second" || m.Snapshot().Goal.Revision != 1 || !m.Snapshot().Armed || findSendMessage(result) == nil {
		t.Fatalf("unexpected snapshot/effects: %+v %+v", m.Snapshot(), result.Effects)
	}
}

func TestGoalReplaceWithNoGoalBehavesLikeSet(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	result := m.Dispatch(GoalReplaceEvent{Objective: "only"})
	if entry := findAppend(result, GoalCustomType); entry == nil || entry.Data.(GoalChangeEntry).Operation != OpCreate {
		t.Fatalf("expected create: %+v", result.Effects)
	}
	if m.Snapshot().Goal.Objective != "only" {
		t.Fatalf("unexpected goal: %+v", m.Snapshot().Goal)
	}
}

func TestGoalReplaceCompletedNeedsNoTombstone(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "first"})
	g := m.Snapshot().Goal
	m.Dispatch(GoalUpdateEvent{GoalID: g.ID, Revision: g.Revision, Action: "complete"})
	result := m.Dispatch(GoalReplaceEvent{Objective: "second"})
	count := 0
	for _, effect := range result.Effects {
		if entry, ok := effect.(AppendEntryEffect); ok && entry.EntryType == GoalCustomType {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected a single create: %+v", result.Effects)
	}
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

func TestTurnEntryUsageIsAccumulated(t *testing.T) {
	m, _ := testMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalCreateEvent{Objective: "ship it"})
	result := m.Dispatch(AgentEndEvent{Usage: &GoalUsage{Tokens: 321, CostUsd: 0.42}})
	entry := findAppend(result, GoalTurnType)
	if entry == nil {
		t.Fatal("expected turn entry")
	}
	turn := entry.Data.(GoalTurnEntry)
	if turn.Usage == nil || turn.Usage.Tokens != 321 || turn.Usage.CostUsd != 0.42 {
		t.Fatalf("unexpected usage: %+v", turn.Usage)
	}
	if m.Snapshot().Goal.UsedTokens != 321 || m.Snapshot().Goal.UsedCostUsd != 0.42 {
		t.Fatalf("unexpected cumulative usage: %+v", m.Snapshot().Goal)
	}
}
