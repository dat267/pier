package goal

import (
	"encoding/json"
	"strings"
	"testing"
)

// Mirrors ~/.pi/agent/extensions/goal/machine.test.ts.

func makeTurnEntry(turn int, goal *GoalSnapshot) CustomEntry {
	return turnEntry(goal.ID, goal.Revision, turn, t0, nil)
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

func machineWithCollector() *recordingMachine {
	machine, _ := testMachine()
	return &recordingMachine{machine: machine}
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
