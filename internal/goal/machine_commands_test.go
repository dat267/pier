package goal

import (
	"encoding/json"
	"strings"
	"testing"
)

// Mirrors ~/.pi/agent/extensions/goal/machine.test.ts.

type fakeClock struct{ ms int64 }

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

type recordingMachine struct {
	machine *GoalMachine
	entries []CustomEntry
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

// coerceRevision accepts only an exact integer revision: a fractional float must
// not match the current revision, and an out-of-range one must not overflow into
// a negative int in the reply.
func TestGoalUpdateRejectsInexactRevision(t *testing.T) {
	machine := NewGoalMachine()
	machine.Dispatch(GoalCreateEvent{Objective: "probe"})
	snapshot := machine.Snapshot()
	if snapshot.Goal == nil {
		t.Fatal("no goal created")
	}
	for _, revision := range []any{1.5, 1e300, -1e300, float64(1<<62) * 4} {
		result := machine.Dispatch(GoalUpdateEvent{GoalID: snapshot.Goal.ID, Revision: revision, Action: "complete"})
		if !result.IsError {
			t.Fatalf("revision %v accepted: %+v", revision, result)
		}
		if result.Reply != nil && strings.Contains(*result.Reply, "-9223372036854775808") {
			t.Fatalf("revision %v overflowed in reply %q", revision, *result.Reply)
		}
	}
	if got := machine.Snapshot().Goal.Phase; got == PhaseComplete {
		t.Fatal("a non-integer revision completed the goal")
	}
	result := machine.Dispatch(GoalUpdateEvent{GoalID: snapshot.Goal.ID, Revision: float64(1), Action: "complete"})
	if result.IsError {
		t.Fatalf("exact integer revision rejected: %+v", result)
	}
}

// blocked against a goal that already finished reported "Cannot block before 3
// consecutive goal rounds (current: 0)": the round-count guard answers first
// and sends the model back to work it has already finished. The reference
// extension reaches neither guard usefully; the port's own refusals name the
// state (D420).
func TestSetGoalBlockedOnAFinishedGoal(t *testing.T) {
	m := NewGoalMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalSetEvent{Objective: "ship it"})
	view := m.Snapshot().Goal
	m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "complete"})

	view = m.Snapshot().Goal
	result := m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "blocked", BlockedReason: "stuck"})
	if !result.IsError {
		t.Fatal("blocking a finished goal must be rejected")
	}
	reply := derefString(result.Reply)
	if strings.Contains(reply, "consecutive goal rounds") {
		t.Fatalf("reply sends the model back to work on a finished goal: %q", reply)
	}
	if !strings.Contains(reply, "already complete") {
		t.Fatalf("reply %q does not name the state", reply)
	}
}

// A second complete on a finished goal reported the phase graph's own
// transition error ("illegal transition complete -> complete"). Every sibling
// refusal in the set_goal status path names the current state and what the
// model can do instead, and this one is reachable: a real session called
// set_goal status=complete on a goal that was already complete. The reply is
// the tool result the model reads, so it must say that the goal has finished
// and that setting an objective starts a new one.
func TestSetGoalCompleteOnAFinishedGoal(t *testing.T) {
	m := NewGoalMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalSetEvent{Objective: "ship it"})
	view := m.Snapshot().Goal
	first := m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "complete"})
	if first.IsError {
		t.Fatalf("first complete rejected: %v", derefString(first.Reply))
	}

	view = m.Snapshot().Goal
	second := m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "complete"})
	if !second.IsError {
		t.Fatal("completing a finished goal must be reported, not silently accepted")
	}
	reply := derefString(second.Reply)
	if strings.Contains(reply, "illegal transition") {
		t.Fatalf("reply leaks the phase graph: %q", reply)
	}
	for _, want := range []string{"already complete", "objective"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("reply %q does not mention %q", reply, want)
		}
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// /goal resume on a finished goal reached the state validator and reported
// "illegal transition complete -> active", the phase graph's wording (D420).
// A finished goal is not a stopped goal, so the guard above says so instead.
func TestGoalResumeOnAFinishedGoal(t *testing.T) {
	m := NewGoalMachine()
	m.Dispatch(SessionStartEvent{})
	m.Dispatch(GoalSetEvent{Objective: "ship it"})
	view := m.Snapshot().Goal
	m.Dispatch(GoalUpdateEvent{GoalID: view.ID, Revision: view.Revision, Action: "complete"})

	result := m.Dispatch(GoalResumeEvent{})
	if !result.IsError {
		t.Fatal("resuming a finished goal must be rejected")
	}
	reply := derefString(result.Reply)
	if strings.Contains(reply, "illegal transition") {
		t.Fatalf("reply leaks the phase graph: %q", reply)
	}
	if !strings.Contains(reply, "complete") {
		t.Fatalf("reply %q does not name the state", reply)
	}
}
