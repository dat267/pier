package goal

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Mirrors goal/machine.test.ts.

var testUsage = ContextUsage{ContextWindow: 100_000}

func createEntry(t *testing.T, objective string) CustomEntry {
	t.Helper()
	goal := CreateGoalState(objective, time.UnixMilli(testT0))
	return changeEntry(t, OpCreate, &goal, testT0, nil)
}

func newMachine(t *testing.T) *GoalMachine {
	t.Helper()
	machine := NewGoalMachine()
	machine.SetClock(func() time.Time { return time.UnixMilli(testT0 + 10_000) })
	return machine
}

func appendEntryOfType(effects []Effect, entryType string) (AppendEntryEffect, bool) {
	for _, effect := range effects {
		if entry, ok := effect.(AppendEntryEffect); ok && entry.EntryType == entryType {
			return entry, true
		}
	}
	return AppendEntryEffect{}, false
}

func effectOf[T Effect](effects []Effect) (T, bool) {
	var zero T
	for _, effect := range effects {
		if typed, ok := effect.(T); ok {
			return typed, true
		}
	}
	return zero, false
}

func TestSessionStart(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	if snapshot := machine.Snapshot(); snapshot.Goal != nil || snapshot.Armed {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	snapshot := machine.Snapshot()
	if snapshot.Goal == nil || snapshot.Goal.Objective != "test" || snapshot.Goal.Phase != PhaseActive || snapshot.Armed {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	goal := CreateGoalState("test", time.UnixMilli(testT0))
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		turnEntry(t, goal.ID, 1, 1, testT0+1),
		turnEntry(t, goal.ID, 1, 2, testT0+2),
	}})
	if got := machine.Snapshot().Goal.TurnsStarted; got != 2 {
		t.Fatalf("turnsStarted = %d", got)
	}
}

func TestGoalCreate(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	result := machine.Dispatch(GoalCreateEvent{Objective: "ship it"})
	entry, ok := effectOf[AppendEntryEffect](result.Effects)
	if !ok || entry.Data.(GoalChangeEntry).Operation != OpCreate {
		t.Fatalf("entry = %+v", entry)
	}
	snapshot := machine.Snapshot()
	if snapshot.Goal == nil || snapshot.Goal.Objective != "ship it" || !snapshot.Armed {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	result = machine.Dispatch(GoalCreateEvent{Objective: "another"})
	if !result.IsError || result.Reply == nil || !strings.Contains(*result.Reply, "already exists") {
		t.Fatalf("result = %+v", result)
	}
	if _, ok := effectOf[AppendEntryEffect](result.Effects); ok {
		t.Fatal("no appendEntry expected")
	}

	// A completed goal may be created over.
	past := time.UnixMilli(testT0)
	old := CreateGoalState("old", past)
	done := old
	done.Phase = PhaseComplete
	done.Revision = 2
	done.UpdatedAt = testT0 + 1
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		changeEntry(t, OpCreate, &old, testT0, nil),
		changeEntry(t, OpComplete, &done, testT0+1, nil),
	}})
	machine.Dispatch(GoalCreateEvent{Objective: "fresh"})
	if got := machine.Snapshot().Goal.Objective; got != "fresh" {
		t.Fatalf("objective = %q", got)
	}
}

func TestAgentEnd(t *testing.T) {
	// createdThisRun admits a turn.
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	machine.Dispatch(GoalCreateEvent{Objective: "ship it"})
	result := machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	turn, ok := effectOf[AppendEntryEffect](result.Effects)
	if !ok || turn.EntryType != GoalTurnType || machine.Snapshot().Goal.TurnsStarted != 1 {
		t.Fatalf("turn = %+v snapshot = %+v", turn, machine.Snapshot())
	}

	// A goal completed in the creating run admits no round.
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	machine.Dispatch(GoalCreateEvent{Objective: "ship it"})
	goal := machine.Snapshot().Goal
	machine.Dispatch(GoalUpdateEvent{GoalID: goal.ID, Revision: goal.Revision, Action: "complete"})
	result = machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	if turn, ok := effectOf[AppendEntryEffect](result.Effects); ok && turn.EntryType == GoalTurnType {
		t.Fatal("completed goal admitted a round")
	}
	if machine.Snapshot().Goal.TurnsStarted != 0 {
		t.Fatal("turnsStarted changed")
	}

	// pendingTurn admits the reserved turn.
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	machine.Dispatch(GoalResumeEvent{})
	if pending := machine.Snapshot().PendingTurn; pending == nil || *pending != 1 {
		t.Fatalf("pendingTurn = %v", pending)
	}
	result = machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	if _, ok := effectOf[AppendEntryEffect](result.Effects); !ok {
		t.Fatal("expected turn admission")
	}
	if machine.Snapshot().PendingTurn != nil || machine.Snapshot().Goal.TurnsStarted != 1 {
		t.Fatalf("snapshot = %+v", machine.Snapshot())
	}

	// An aborted goal attempt pauses.
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	machine.Dispatch(GoalResumeEvent{})
	result = machine.Dispatch(AgentEndEvent{ContextUsage: testUsage, Aborted: true})
	pause, ok := appendEntryOfType(result.Effects, GoalCustomType)
	if !ok || pause.Data.(GoalChangeEntry).Operation != OpPause {
		t.Fatalf("pause = %+v", pause)
	}
	if machine.Snapshot().Goal.Phase != PhasePaused || machine.Snapshot().Armed {
		t.Fatalf("snapshot = %+v", machine.Snapshot())
	}

	// An aborted non-attempt only disarms.
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	result = machine.Dispatch(AgentEndEvent{ContextUsage: testUsage, Aborted: true})
	if _, ok := effectOf[AppendEntryEffect](result.Effects); ok {
		t.Fatal("unexpected entry")
	}
	if machine.Snapshot().Armed {
		t.Fatal("not disarmed")
	}
}

func TestAgentSettled(t *testing.T) {
	// Armed active goal under the cap queues a round.
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	machine.Dispatch(GoalResumeEvent{})
	machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	result := machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage})
	message, ok := effectOf[SendMessageEffect](result.Effects)
	if !ok || !message.TriggerTurn || machine.Snapshot().PendingTurn == nil || *machine.Snapshot().PendingTurn != 2 {
		t.Fatalf("message = %+v snapshot = %+v", message, machine.Snapshot())
	}

	// The first provider error schedules a 30s retry.
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	machine.Dispatch(GoalResumeEvent{})
	machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	result = machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage, ProviderError: &ProviderError{Status: 429, Message: "HTTP 429"}})
	retry, ok := effectOf[ScheduleRetryEffect](result.Effects)
	if !ok || retry.DelayMs != 30_000 {
		t.Fatalf("retry = %+v", retry)
	}
	if machine.Snapshot().Goal.Phase != PhaseActive {
		t.Fatal("paused on the first transient error")
	}
	if _, ok := effectOf[SendMessageEffect](result.Effects); ok {
		t.Fatal("round queued on an error")
	}
}

func TestAgentSettledRetryLimit(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	machine.Dispatch(GoalResumeEvent{})
	machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	settle := func() DispatchResult {
		return machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage, ProviderError: &ProviderError{Status: 429, Message: "You have reached your 5-hour Clinepass limit."}})
	}
	settle()
	settle()
	settle()
	result := settle()
	if _, ok := effectOf[SendMessageEffect](result.Effects); ok {
		t.Fatal("continuation round after retry limit")
	}
	if _, ok := effectOf[ScheduleRetryEffect](result.Effects); ok {
		t.Fatal("retry past the limit")
	}
	snapshot := machine.Snapshot()
	if snapshot.Armed || snapshot.Goal.Phase != PhasePaused || snapshot.Goal.BlockedReason.Code != "api-error" || !strings.Contains(snapshot.Goal.BlockedReason.Message, "429") {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestAgentSettledBackoffEscalation(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	machine.Dispatch(GoalResumeEvent{})
	machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	settle := func(retryAfterMs *int64) ScheduleRetryEffect {
		result := machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage, ProviderError: &ProviderError{Status: 429, Message: "HTTP 429", RetryAfterMs: retryAfterMs}})
		retry, _ := effectOf[ScheduleRetryEffect](result.Effects)
		return retry
	}
	server := int64(300_000)
	retry := settle(&server)
	if retry.DelayMs != 300_000 || retry.Attempt != 1 {
		t.Fatalf("retry = %+v", retry)
	}
	if retry := settle(nil); retry.DelayMs != 60_000 || retry.Attempt != 2 {
		t.Fatalf("retry = %+v", retry)
	}
	if retry := settle(nil); retry.DelayMs != 120_000 || retry.Attempt != 3 {
		t.Fatalf("retry = %+v", retry)
	}
}

func TestAgentSettledPermanentErrors(t *testing.T) {
	codeFor := func(status int) string {
		machine := newMachine(t)
		machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
		machine.Dispatch(GoalResumeEvent{})
		machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
		result := machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage, ProviderError: &ProviderError{Status: status, Message: "HTTP " + itoa(status)}})
		if _, ok := effectOf[ScheduleRetryEffect](result.Effects); ok {
			t.Fatalf("status %d retried", status)
		}
		if _, ok := effectOf[SendMessageEffect](result.Effects); ok {
			t.Fatalf("status %d queued a round", status)
		}
		if machine.Snapshot().Armed {
			t.Fatalf("status %d not disarmed", status)
		}
		if machine.Snapshot().Goal.Phase != PhasePaused {
			t.Fatalf("status %d not paused", status)
		}
		return machine.Snapshot().Goal.BlockedReason.Code
	}
	for status, want := range map[int]string{401: "api-auth", 403: "api-auth", 402: "api-billing", 400: "api-request", 404: "api-request", 422: "api-request"} {
		if got := codeFor(status); got != want {
			t.Fatalf("status %d: code = %q, want %q", status, got, want)
		}
	}
}

func TestRetryableStatusesTakeTheBackoffSchedule(t *testing.T) {
	for _, status := range []int{408, 409, 425, 429, 500, 502, 503} {
		machine := newMachine(t)
		machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
		machine.Dispatch(GoalResumeEvent{})
		machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
		result := machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage, ProviderError: &ProviderError{Status: status, Message: "HTTP " + itoa(status)}})
		if _, ok := effectOf[ScheduleRetryEffect](result.Effects); !ok {
			t.Fatalf("status %d did not retry", status)
		}
		if machine.Snapshot().Goal.Phase != PhaseActive {
			t.Fatalf("status %d paused on the first failure", status)
		}
	}
}

func TestSuccessfulSettleResetsTheRetryCounter(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	machine.Dispatch(GoalResumeEvent{})
	machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	settleErr := func() DispatchResult {
		return machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage, ProviderError: &ProviderError{Status: 500, Message: "HTTP 500"}})
	}
	settleErr()
	settleErr()
	machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage})
	machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	settleErr()
	settleErr()
	settleErr()
	result := settleErr()
	if _, ok := effectOf[ScheduleRetryEffect](result.Effects); ok {
		t.Fatal("counter was reset")
	}
	if machine.Snapshot().Goal.Phase != PhasePaused {
		t.Fatal("not paused")
	}
}

func TestRetryDue(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	machine.Dispatch(GoalResumeEvent{})
	machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage, ProviderError: &ProviderError{Status: 429, Message: "HTTP 429"}})
	result := machine.Dispatch(RetryDueEvent{})
	if _, ok := effectOf[SendMessageEffect](result.Effects); !ok {
		t.Fatal("retry_due did not queue the round")
	}
	if machine.Snapshot().PendingTurn == nil || *machine.Snapshot().PendingTurn != 2 {
		t.Fatalf("pendingTurn = %v", machine.Snapshot().PendingTurn)
	}

	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}})
	result = machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage})
	if _, ok := effectOf[SendMessageEffect](result.Effects); ok {
		t.Fatal("disarmed machine queued a round")
	}
}

func TestGoalUpdate(t *testing.T) {
	armed := func() *GoalMachine {
		machine := newMachine(t)
		machine.Dispatch(SessionStartEvent{})
		machine.Dispatch(GoalCreateEvent{Objective: "ship it"})
		return machine
	}

	machine := armed()
	goal := machine.Snapshot().Goal
	result := machine.Dispatch(GoalUpdateEvent{GoalID: goal.ID, Revision: goal.Revision, Action: "complete"})
	if result.IsError || result.Reply == nil || !strings.Contains(*result.Reply, "complete") {
		t.Fatalf("result = %+v", result)
	}
	entry, _ := effectOf[AppendEntryEffect](result.Effects)
	if entry.Data.(GoalChangeEntry).Operation != OpComplete {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := effectOf[SendMessageEffect](result.Effects); !ok {
		t.Fatal("wrapped message missing")
	}
	if machine.Snapshot().Goal.Phase != PhaseComplete || machine.Snapshot().Armed {
		t.Fatalf("snapshot = %+v", machine.Snapshot())
	}

	machine = armed()
	goal = machine.Snapshot().Goal
	result = machine.Dispatch(GoalUpdateEvent{GoalID: goal.ID, Revision: 999, Action: "complete"})
	if !result.IsError || !strings.Contains(*result.Reply, "revision 999") || !strings.Contains(*result.Reply, "current is 1") {
		t.Fatalf("result = %+v", result)
	}
	if machine.Snapshot().Goal.Phase != PhaseActive {
		t.Fatal("mutated on a stale ref")
	}

	machine = armed()
	result = machine.Dispatch(GoalUpdateEvent{GoalID: "goal_9f5b6c48e33d", Revision: 1, Action: "complete"})
	if !result.IsError || !strings.Contains(*result.Reply, machine.Snapshot().Goal.ID) || !strings.Contains(*result.Reply, "rev 1") {
		t.Fatalf("result = %+v", result)
	}

	machine = armed()
	result = machine.Dispatch(GoalUpdateEvent{GoalID: "any", Revision: 1, Action: "pause"})
	if !result.IsError || !strings.Contains(*result.Reply, `Unknown action "pause"`) {
		t.Fatalf("result = %+v", result)
	}

	machine = armed()
	for _, bad := range []any{nil, "abc", math.NaN()} {
		result = machine.Dispatch(GoalUpdateEvent{GoalID: "any", Revision: bad, Action: "complete"})
		if !result.IsError || !strings.Contains(*result.Reply, "revision") {
			t.Fatalf("bad revision %v: result = %+v", bad, result)
		}
	}

	// Blocked before 3 rounds is rejected.
	machine = armed()
	goal = machine.Snapshot().Goal
	result = machine.Dispatch(GoalUpdateEvent{GoalID: goal.ID, Revision: goal.Revision, Action: "blocked", BlockedReason: "stuck"})
	if !result.IsError || !strings.Contains(*result.Reply, "3 consecutive") || machine.Snapshot().Goal.Phase != PhaseActive {
		t.Fatalf("result = %+v snapshot = %+v", result, machine.Snapshot())
	}

	// Blocked after 3 rounds.
	machine = armed()
	for round := 0; round < 3; round++ {
		machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
		machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage})
	}
	machine.Dispatch(AgentEndEvent{ContextUsage: testUsage})
	goal = machine.Snapshot().Goal
	result = machine.Dispatch(GoalUpdateEvent{GoalID: goal.ID, Revision: goal.Revision, Action: "blocked", BlockedReason: "stuck"})
	if result.IsError || !strings.Contains(*result.Reply, "blocked") {
		t.Fatalf("result = %+v", result)
	}
	entry, _ = effectOf[AppendEntryEffect](result.Effects)
	if entry.Data.(GoalChangeEntry).Operation != OpBlock || machine.Snapshot().Goal.Phase != PhaseBlocked {
		t.Fatalf("entry = %+v snapshot = %+v", entry, machine.Snapshot())
	}
}

func TestCommands(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	machine.Dispatch(GoalCreateEvent{Objective: "ship it"})
	result := machine.Dispatch(GoalPauseEvent{})
	entry, _ := effectOf[AppendEntryEffect](result.Effects)
	if entry.Data.(GoalChangeEntry).Operation != OpPause || machine.Snapshot().Goal.Phase != PhasePaused || machine.Snapshot().Armed {
		t.Fatalf("entry = %+v snapshot = %+v", entry, machine.Snapshot())
	}

	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	machine.Dispatch(GoalCreateEvent{Objective: "ship it"})
	goal := machine.Snapshot().Goal
	result = machine.Dispatch(GoalClearEvent{ID: goal.ID, Revision: goal.Revision})
	entry, _ = effectOf[AppendEntryEffect](result.Effects)
	if entry.Data.(GoalChangeEntry).Operation != OpClear || machine.Snapshot().Goal != nil {
		t.Fatalf("entry = %+v snapshot = %+v", entry, machine.Snapshot())
	}

	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	if machine.Snapshot().BannerEnabled {
		t.Fatal("banner default on")
	}
	result = machine.Dispatch(BannerToggleEvent{})
	if !machine.Snapshot().BannerEnabled {
		t.Fatal("banner not toggled")
	}
	if _, ok := effectOf[RenderStatusEffect](result.Effects); !ok {
		t.Fatal("renderStatus missing")
	}

	// commit validates through applyChange: an illegal live transition errors
	// and leaves state untouched.
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	machine.Dispatch(GoalCreateEvent{Objective: "x"})
	goal = machine.Snapshot().Goal
	machine.Dispatch(GoalUpdateEvent{GoalID: goal.ID, Revision: goal.Revision, Action: "complete"})
	completed := machine.Snapshot().Goal
	illegal := completed.GoalSnapshot
	illegal.Phase = PhasePaused
	illegal.Revision = completed.Revision + 1
	illegal.BlockedReason = &BlockedReason{Code: "x", Message: "y"}
	if _, err := machine.commit(OpPause, &illegal, nil); err == nil || !strings.Contains(err.Error(), "illegal transition") {
		t.Fatalf("err = %v", err)
	}
	if machine.Snapshot().Goal.Phase != PhaseComplete || machine.Snapshot().Goal.Revision != completed.Revision {
		t.Fatal("rejected commit mutated state")
	}

	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	result = machine.Dispatch(GoalSetEvent{Objective: "from command"})
	entry, _ = effectOf[AppendEntryEffect](result.Effects)
	if entry.Data.(GoalChangeEntry).Operation != OpCreate {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := effectOf[SendMessageEffect](result.Effects); !ok {
		t.Fatal("immediate round missing")
	}
	if machine.Snapshot().PendingTurn == nil || *machine.Snapshot().PendingTurn != 1 || machine.Snapshot().Goal.Objective != "from command" {
		t.Fatalf("snapshot = %+v", machine.Snapshot())
	}
}

func TestSessionStartCorruption(t *testing.T) {
	goal := CreateGoalState("test", time.UnixMilli(testT0))
	corrupt := goal
	corrupt.Phase = PhaseActive
	corrupt.Revision = 5
	corrupt.UpdatedAt = testT0 + 1
	machine := newMachine(t)
	result := machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		changeEntry(t, OpResume, &corrupt, testT0+1, nil),
	}})
	if machine.Snapshot().Goal != nil {
		t.Fatal("corrupt goal not dropped")
	}
	notify, ok := effectOf[NotifyEffect](result.Effects)
	if !ok || notify.Level != "warning" || !strings.Contains(strings.ToLower(notify.Message), "corrupt") {
		t.Fatalf("notify = %+v", notify)
	}
	if result.Reply != nil {
		t.Fatalf("reply = %v", *result.Reply)
	}
}

func TestRoundTripWriteShapeReplays(t *testing.T) {
	type collector struct {
		machine *GoalMachine
		entries []CustomEntry
	}
	build := func() *collector {
		machine := newMachine(t)
		collected := &collector{machine: machine}
		return collected
	}
	dispatch := func(c *collector, event GoalEvent) DispatchResult {
		result := c.machine.Dispatch(event)
		for _, effect := range result.Effects {
			if entry, ok := effect.(AppendEntryEffect); ok {
				collected := CustomEntry{CustomType: entry.EntryType}
				data, _ := json.Marshal(entry.Data)
				collected.Data = data
				c.entries = append(c.entries, collected)
			}
		}
		return result
	}

	// set-over-complete replays the second goal.
	c := build()
	dispatch(c, SessionStartEvent{})
	dispatch(c, GoalCreateEvent{Objective: "first"})
	first := c.machine.Snapshot().Goal
	dispatch(c, GoalUpdateEvent{GoalID: first.ID, Revision: first.Revision, Action: "complete"})
	dispatch(c, GoalSetEvent{Objective: "second"})
	second := c.machine.Snapshot().Goal
	fresh := newMachine(t)
	fresh.Dispatch(SessionStartEvent{Entries: c.entries})
	if got := fresh.Snapshot().Goal; got == nil || got.ID != second.ID || got.Objective != "second" || got.Phase != PhaseActive {
		t.Fatalf("replayed = %+v", got)
	}

	// Full lifecycle replay.
	c = build()
	dispatch(c, SessionStartEvent{})
	dispatch(c, GoalCreateEvent{Objective: "round trip"})
	dispatch(c, AgentEndEvent{ContextUsage: testUsage})
	dispatch(c, AgentSettledEvent{ContextUsage: testUsage})
	dispatch(c, AgentEndEvent{ContextUsage: testUsage})
	dispatch(c, AgentSettledEvent{ContextUsage: testUsage})
	dispatch(c, AgentEndEvent{ContextUsage: testUsage})
	dispatch(c, GoalPauseEvent{})
	dispatch(c, GoalResumeEvent{})
	dispatch(c, AgentEndEvent{ContextUsage: testUsage})
	snapBefore := c.machine.Snapshot().Goal
	dispatch(c, GoalUpdateEvent{GoalID: snapBefore.ID, Revision: snapBefore.Revision, Action: "complete"})
	snap := c.machine.Snapshot().Goal
	fresh = newMachine(t)
	fresh.Dispatch(SessionStartEvent{Entries: c.entries})
	replayed := fresh.Snapshot().Goal
	if replayed.ID != snap.ID || replayed.Revision != snap.Revision || replayed.Objective != snap.Objective || replayed.Phase != PhaseComplete || replayed.TurnsStarted != snap.TurnsStarted {
		t.Fatalf("replayed = %+v snap = %+v", replayed, snap)
	}

	// Mid-lifecycle pause preserves phase + reason.
	c = build()
	dispatch(c, SessionStartEvent{})
	dispatch(c, GoalCreateEvent{Objective: "pause me"})
	dispatch(c, GoalPauseEvent{})
	fresh = newMachine(t)
	fresh.Dispatch(SessionStartEvent{Entries: c.entries})
	g := fresh.Snapshot().Goal
	if g.Phase != PhasePaused || g.BlockedReason.Code != "human-paused" || g.TurnsStarted != 0 {
		t.Fatalf("replayed = %+v", g)
	}

	// Non-goal custom entries are ignored.
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		{CustomType: "pi-todo", Data: []byte(`{"noise":true}`)},
		createEntry(t, "test"),
		{CustomType: GoalEventType, Data: []byte(`{"noise":true}`)},
	}})
	if got := machine.Snapshot().Goal.Objective; got != "test" {
		t.Fatalf("objective = %q", got)
	}
}

func TestSessionStartReload(t *testing.T) {
	machine := newMachine(t)
	result := machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}, Reason: "reload"})
	entry, ok := effectOf[AppendEntryEffect](result.Effects)
	if !ok || entry.Data.(GoalChangeEntry).Operation != OpPause || entry.Data.(GoalChangeEntry).Goal.BlockedReason.Code != "reloaded" {
		t.Fatalf("entry = %+v", entry)
	}
	snapshot := machine.Snapshot()
	if snapshot.Goal.Phase != PhasePaused || snapshot.Armed {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if _, ok := effectOf[SendMessageEffect](result.Effects); ok {
		t.Fatal("reload started a turn")
	}
	notify, _ := effectOf[NotifyEffect](result.Effects)
	if !strings.Contains(notify.Message, "reload") || !strings.Contains(notify.Message, "/goal resume") {
		t.Fatalf("notify = %+v", notify)
	}

	// Clock skew: a rejected reload pause keeps the goal.
	future := CreateGoalState("test", time.UnixMilli(testT0+60_000))
	machine = newMachine(t)
	result = machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{changeEntry(t, OpCreate, &future, testT0+60_000, nil)}, Reason: "reload"})
	if machine.Snapshot().Goal == nil || machine.Snapshot().Goal.ID != future.ID || machine.Snapshot().Goal.Phase != PhaseActive {
		t.Fatalf("snapshot = %+v", machine.Snapshot())
	}
	notify, ok = effectOf[NotifyEffect](result.Effects)
	if !ok || notify.Level != "warning" || !strings.Contains(notify.Message, "not paused") {
		t.Fatalf("notify = %+v", notify)
	}

	// An already-paused goal is left alone.
	goal := CreateGoalState("test", time.UnixMilli(testT0))
	paused := goal
	paused.Phase = PhasePaused
	paused.Revision = 2
	paused.BlockedReason = &BlockedReason{Code: "human-paused", Message: "Paused by user."}
	paused.UpdatedAt = testT0 + 1
	machine = newMachine(t)
	result = machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		changeEntry(t, OpCreate, &goal, testT0, nil),
		changeEntry(t, OpPause, &paused, testT0+1, nil),
	}, Reason: "reload"})
	if _, ok := effectOf[AppendEntryEffect](result.Effects); ok {
		t.Fatal("reload wrote an entry for a paused goal")
	}
	if machine.Snapshot().Goal.Phase != PhasePaused {
		t.Fatalf("phase = %s", machine.Snapshot().Goal.Phase)
	}

	// startup/resume/new/fork keep the phase, merely disarmed.
	for _, reason := range []string{"startup", "resume", "new", "fork"} {
		machine = newMachine(t)
		result = machine.Dispatch(SessionStartEvent{Entries: []CustomEntry{createEntry(t, "test")}, Reason: reason})
		if _, ok := effectOf[AppendEntryEffect](result.Effects); ok {
			t.Fatalf("%s wrote an entry", reason)
		}
		if machine.Snapshot().Goal.Phase != PhaseActive || machine.Snapshot().Armed {
			t.Fatalf("%s snapshot = %+v", reason, machine.Snapshot())
		}
	}
}

func TestBannerPersistence(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	result := machine.Dispatch(BannerToggleEvent{})
	entry, ok := effectOf[AppendEntryEffect](result.Effects)
	if !ok || entry.EntryType != GoalSettingsType || !entry.Data.(GoalSettingsEntry).BannerEnabled {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := effectOf[RenderStatusEffect](result.Effects); !ok {
		t.Fatal("renderStatus missing")
	}

	fresh := newMachine(t)
	fresh.Dispatch(SessionStartEvent{Entries: []CustomEntry{
		createEntry(t, "test"),
		settingsEntry(t, true, testT0+1),
	}})
	if !fresh.Snapshot().BannerEnabled {
		t.Fatal("banner not restored")
	}
}

func TestGoalReplace(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	machine.Dispatch(GoalCreateEvent{Objective: "first"})
	before := machine.Snapshot().Goal
	result := machine.Dispatch(GoalReplaceEvent{Objective: "second"})
	entries := make([]GoalChangeEntry, 0, 2)
	for _, effect := range result.Effects {
		if entry, ok := effect.(AppendEntryEffect); ok {
			entries = append(entries, entry.Data.(GoalChangeEntry))
		}
	}
	if len(entries) != 2 || entries[0].Operation != OpClear || entries[1].Operation != OpCreate {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Cleared.ID != before.ID || entries[0].Cleared.Revision != before.Revision {
		t.Fatalf("cleared = %+v", entries[0].Cleared)
	}
	snapshot := machine.Snapshot()
	if snapshot.Goal.Objective != "second" || snapshot.Goal.Revision != 1 || !snapshot.Armed {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if _, ok := effectOf[SendMessageEffect](result.Effects); !ok {
		t.Fatal("immediate round missing")
	}

	// With no goal it behaves like goal_set.
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	result = machine.Dispatch(GoalReplaceEvent{Objective: "only"})
	entries = entries[:0]
	for _, effect := range result.Effects {
		if entry, ok := effect.(AppendEntryEffect); ok {
			entries = append(entries, entry.Data.(GoalChangeEntry))
		}
	}
	if len(entries) != 1 || entries[0].Operation != OpCreate || machine.Snapshot().Goal.Objective != "only" {
		t.Fatalf("entries = %+v", entries)
	}

	// Replacing a completed goal needs no tombstone.
	machine = newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	machine.Dispatch(GoalCreateEvent{Objective: "first"})
	goal := machine.Snapshot().Goal
	machine.Dispatch(GoalUpdateEvent{GoalID: goal.ID, Revision: goal.Revision, Action: "complete"})
	result = machine.Dispatch(GoalReplaceEvent{Objective: "second"})
	entries = entries[:0]
	for _, effect := range result.Effects {
		if entry, ok := effect.(AppendEntryEffect); ok {
			entries = append(entries, entry.Data.(GoalChangeEntry))
		}
	}
	if len(entries) != 1 || entries[0].Operation != OpCreate {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestAgentSettledUnderUserInput(t *testing.T) {
	machine := newMachine(t)
	machine.Dispatch(SessionStartEvent{})
	machine.Dispatch(GoalCreateEvent{Objective: "do it"})
	result := machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage, HasPendingMessages: true})
	if _, ok := effectOf[SendMessageEffect](result.Effects); ok {
		t.Fatal("pending user input did not suppress the round")
	}
	if machine.Snapshot().PendingTurn != nil {
		t.Fatal("pendingTurn set")
	}

	result = machine.Dispatch(AgentSettledEvent{ContextUsage: testUsage, HasPendingMessages: false})
	if _, ok := effectOf[SendMessageEffect](result.Effects); !ok {
		t.Fatal("round not queued")
	}
}

func itoa(value int) string { return strconv.Itoa(value) }
