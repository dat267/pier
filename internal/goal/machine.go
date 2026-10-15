// This file follows goal/machine.ts: GoalMachine, the deep module owning all
// goal state. One seam: Dispatch(event) -> { effects, reply, isError }. Effects
// are plain data; the caller performs all I/O.
package goal

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// BlockedAfterTurns is the number of consecutive rounds before update_goal may
// block.
const BlockedAfterTurns = 3

// ContextUsage is the context-window usage reported at agent_end (the current
// extension ignores it; kept for the event shape).
type ContextUsage struct {
	Tokens        *int64
	ContextWindow int64
}

// GoalEvent is one machine input. Concrete types implement isGoalEvent.
type GoalEvent interface{ isGoalEvent() }

// SessionStartEvent replays durable entries at session start.
type SessionStartEvent struct {
	Entries []CustomEntry
	Reason  string
}

// GoalCreateEvent is the create_goal tool.
type GoalCreateEvent struct {
	Objective string
	Budget    *GoalBudget
}

// GoalResumeEvent is /goal resume.
type GoalResumeEvent struct{}

// AgentEndEvent is the agent_end lifecycle hook.
type AgentEndEvent struct {
	ContextUsage ContextUsage
	Usage        *GoalUsage
	Aborted      bool
}

// AgentBeforeSettleEvent is the agent_before_settle lifecycle hook.
type AgentBeforeSettleEvent struct {
	Outcome            string
	HasPendingMessages bool
	Now                int64
	HasNow             bool
}

// GoalUpdateEvent is the update_goal tool.
type GoalUpdateEvent struct {
	GoalID        string
	Revision      any // number | string | undefined
	Action        string
	BlockedReason string
}

// GoalPauseEvent is /goal pause.
type GoalPauseEvent struct{}

// GoalClearEvent is /goal clear.
type GoalClearEvent struct {
	ID       string
	Revision int
}

// GoalSetEvent is /goal set.
type GoalSetEvent struct {
	Objective string
	Budget    *GoalBudget
}

// GoalReplaceEvent is /goal set over a live goal (human-confirmed).
type GoalReplaceEvent struct {
	Objective string
	Budget    *GoalBudget
}

func (SessionStartEvent) isGoalEvent()      {}
func (GoalCreateEvent) isGoalEvent()        {}
func (GoalResumeEvent) isGoalEvent()        {}
func (AgentEndEvent) isGoalEvent()          {}
func (AgentBeforeSettleEvent) isGoalEvent() {}
func (GoalUpdateEvent) isGoalEvent()        {}
func (GoalPauseEvent) isGoalEvent()         {}
func (GoalClearEvent) isGoalEvent()         {}
func (GoalSetEvent) isGoalEvent()           {}
func (GoalReplaceEvent) isGoalEvent()       {}

// Effect is one machine output. The caller performs the I/O.
type Effect interface{ isEffect() }

// AppendEntryEffect persists a durable custom entry.
type AppendEntryEffect struct {
	EntryType string
	Data      any
}

// SendMessageEffect sends a continuation or wrap-up message.
type SendMessageEffect struct {
	CustomType  string
	Content     string
	Display     bool
	Details     map[string]any
	TriggerTurn bool
}

// NotifyEffect surfaces a notification to the human.
type NotifyEffect struct {
	Message string
	Level   string // "info" | "warning"
}

// ContinueRoundEffect requests continuation inside the run with a round prompt.
type ContinueRoundEffect struct {
	Content string
	Turn    int
}

func (AppendEntryEffect) isEffect()   {}
func (SendMessageEffect) isEffect()   {}
func (NotifyEffect) isEffect()        {}
func (ContinueRoundEffect) isEffect() {}

// DispatchResult is one dispatch's outcome.
type DispatchResult struct {
	Effects []Effect
	// Reply is text the caller surfaces to a tool result (nil = no reply).
	Reply *string
	// IsError marks Reply as an error.
	IsError bool
	// Continue requests the run continue with the round prompt.
	Continue bool
}

// MachineSnapshot is the machine's observable state.
type MachineSnapshot struct {
	Goal           *GoalView
	Armed          bool
	PendingTurn    *int
	CreatedThisRun bool
}

// GoalMachine owns all goal state.
type GoalMachine struct {
	view           *GoalView
	armed          bool
	pendingTurn    *int
	createdThisRun bool

	now func() time.Time
}

// NewGoalMachine creates a machine with the real clock.
func NewGoalMachine() *GoalMachine {
	return &GoalMachine{now: time.Now}
}

// SetClock overrides the machine clock (tests).
func (m *GoalMachine) SetClock(now func() time.Time) { m.now = now }

// Snapshot returns the current observable state.
func (m *GoalMachine) Snapshot() MachineSnapshot {
	var goal *GoalView
	if m.view != nil {
		copied := *m.view
		copied.Armed = m.armed
		goal = &copied
	}
	return MachineSnapshot{
		Goal:           goal,
		Armed:          m.armed,
		PendingTurn:    m.pendingTurn,
		CreatedThisRun: m.createdThisRun,
	}
}

// Dispatch advances the machine for one event.
func (m *GoalMachine) Dispatch(event GoalEvent) DispatchResult {
	switch typed := event.(type) {
	case SessionStartEvent:
		return m.sessionStart(typed.Entries, typed.Reason)
	case GoalCreateEvent:
		return m.goalCreate(typed.Objective, typed.Budget)
	case GoalResumeEvent:
		return m.goalResume()
	case AgentEndEvent:
		return m.agentEnd(typed.ContextUsage, typed.Usage, typed.Aborted)
	case AgentBeforeSettleEvent:
		now := m.now().UnixMilli()
		if typed.HasNow {
			now = typed.Now
		}
		return m.agentBeforeSettle(typed.Outcome, typed.HasPendingMessages, now)
	case GoalUpdateEvent:
		return m.goalUpdate(typed.GoalID, typed.Revision, typed.Action, typed.BlockedReason)
	case GoalPauseEvent:
		return m.goalPause()
	case GoalClearEvent:
		return m.goalClear(typed.ID, typed.Revision)
	case GoalSetEvent:
		return m.goalSet(typed.Objective, typed.Budget)
	case GoalReplaceEvent:
		return m.goalReplace(typed.Objective, typed.Budget)
	}
	return DispatchResult{}
}

// budgetStopReason reports the first exhausted budget limit.
func budgetStopReason(goal *GoalView, now int64) *BlockedReason {
	budget := goal.Budget
	if budget == nil {
		return nil
	}
	if budget.TimeLimitMs != nil && now >= goal.CreatedAt+*budget.TimeLimitMs {
		return &BlockedReason{Code: "budget-time", Message: "Time limit reached."}
	}
	if budget.TokenLimit != nil && goal.UsedTokens >= *budget.TokenLimit {
		return &BlockedReason{Code: "budget-tokens", Message: "Token limit reached."}
	}
	if budget.CostLimitUsd != nil && goal.UsedCostUsd >= *budget.CostLimitUsd {
		return &BlockedReason{Code: "budget-cost", Message: "Reported cost limit reached."}
	}
	return nil
}

func (m *GoalMachine) wrapup(kind, objective, blockedReason string) Effect {
	return SendMessageEffect{
		CustomType:  GoalEventType,
		Content:     WrapupContext(objective, blockedReason),
		Display:     false,
		Details:     map[string]any{"kind": kind},
		TriggerTurn: false,
	}
}

func (m *GoalMachine) goalUpdate(goalID string, rawRevision any, rawAction, blockedReason string) DispatchResult {
	if rawAction != "complete" && rawAction != "blocked" {
		return DispatchResult{Reply: stringPtr(fmt.Sprintf(`Unknown action %s. Use "complete" or "blocked".`, jsonQuote(rawAction))), IsError: true}
	}
	revision, ok := coerceRevision(rawRevision)
	if !ok {
		current := "n/a"
		if m.view != nil {
			current = strconv.Itoa(m.view.Revision)
		}
		return DispatchResult{Reply: stringPtr(fmt.Sprintf("revision is required — the exact number from get_goal (current goal revision: %s).", current)), IsError: true}
	}
	if m.view == nil {
		return DispatchResult{Reply: stringPtr("No goal is set."), IsError: true}
	}
	if goalID != m.view.ID {
		// Name the current ref — the model can retry without a get_goal round trip.
		return DispatchResult{Reply: stringPtr(fmt.Sprintf(`Unknown goal id "%s". Current goal: %s rev %d. Retry with these values.`, goalID, m.view.ID, m.view.Revision)), IsError: true}
	}
	if revision != m.view.Revision {
		return DispatchResult{Reply: stringPtr(fmt.Sprintf("Stale ref: you sent revision %d, current is %d (id %s). Retry with these values.", revision, m.view.Revision, m.view.ID)), IsError: true}
	}

	if rawAction == "complete" {
		next := m.view.GoalSnapshot
		next.Phase = PhaseComplete
		next.BlockedReason = nil
		next.Revision = m.view.Revision + 1
		next.UpdatedAt = m.now().UnixMilli()
		m.armed = false
		m.pendingTurn = nil
		effects, err := m.commit(OpComplete, &next, nil)
		if err != nil {
			return DispatchResult{Reply: stringPtr(err.Error()), IsError: true}
		}
		effects = append(effects, m.wrapup("complete", next.Objective, ""))
		return DispatchResult{Effects: effects, Reply: stringPtr("Goal marked complete. Stop goal work.")}
	}

	// action == "blocked"
	if m.view.TurnsStarted < BlockedAfterTurns {
		return DispatchResult{Reply: stringPtr(fmt.Sprintf("Cannot block before %d consecutive goal rounds (current: %d). Keep working or try a different approach.", BlockedAfterTurns, m.view.TurnsStarted)), IsError: true}
	}
	reason := strings.TrimSpace(blockedReason)
	if reason == "" {
		return DispatchResult{Reply: stringPtr("blocked_reason is required."), IsError: true}
	}
	stop := &BlockedReason{Code: "model-reported", Message: reason}
	next := m.view.GoalSnapshot
	next.Phase = PhaseBlocked
	next.BlockedReason = stop
	next.Revision = m.view.Revision + 1
	next.UpdatedAt = m.now().UnixMilli()
	m.armed = false
	effects, err := m.commit(OpBlock, &next, nil)
	if err != nil {
		return DispatchResult{Reply: stringPtr(err.Error()), IsError: true}
	}
	effects = append(effects, m.wrapup("blocked", next.Objective, reason))
	return DispatchResult{Effects: effects, Reply: stringPtr("Goal blocked. Stop goal work.")}
}

// queueRound reserves the next round: set pendingTurn and emit the prompt.
func (m *GoalMachine) queueRound() []Effect {
	if m.view == nil {
		return nil
	}
	turn := m.view.TurnsStarted + 1
	m.pendingTurn = &turn
	return []Effect{
		SendMessageEffect{
			CustomType:  GoalEventType,
			Content:     GoalRoundPrompt(*m.view, turn, m.now()),
			Display:     false,
			Details:     map[string]any{"kind": "round", "turn": turn},
			TriggerTurn: true,
		},
	}
}

func (m *GoalMachine) commit(operation GoalOperation, next *GoalSnapshot, cleared *ClearedRef) ([]Effect, error) {
	var data GoalChangeEntry
	if cleared != nil {
		data = GoalChangeEntry{Operation: operation, Cleared: cleared, Timestamp: m.now().UnixMilli()}
	} else {
		var clean *GoalSnapshot
		if next != nil {
			snapshot := ToSnapshot(*next)
			clean = &snapshot
		}
		data = GoalChangeEntry{Operation: operation, Goal: clean, Timestamp: m.now().UnixMilli()}
	}
	var current *GoalSnapshot
	if m.view != nil {
		snapshot := ToSnapshot(m.view.GoalSnapshot)
		current = &snapshot
	}
	validated, err := ApplyChange(current, data)
	if err != nil {
		return nil, err
	}
	var view *GoalView
	if validated != nil {
		next := GoalView{GoalSnapshot: *validated, Armed: m.armed}
		if m.view != nil && m.view.ID == validated.ID {
			next.TurnsStarted = m.view.TurnsStarted
			next.UsedTokens = m.view.UsedTokens
			next.UsedCostUsd = m.view.UsedCostUsd
		}
		view = &next
	}
	m.view = view
	return []Effect{AppendEntryEffect{EntryType: GoalCustomType, Data: data}}, nil
}

func (m *GoalMachine) goalCreate(objective string, budget *GoalBudget) DispatchResult {
	if m.view != nil && m.view.Phase != PhaseComplete {
		return DispatchResult{Reply: stringPtr("A goal already exists. Clear it first."), IsError: true}
	}
	next := CreateGoalState(objective, m.now(), budget)
	m.armed = true
	m.createdThisRun = true
	effects, err := m.commit(OpCreate, &next, nil)
	if err != nil {
		return DispatchResult{Reply: stringPtr(err.Error()), IsError: true}
	}
	// The id must reach model context here — the completion call depends on it.
	return DispatchResult{Effects: effects, Reply: stringPtr(fmt.Sprintf("Goal created (id %s, revision %d).", next.ID, next.Revision))}
}

func (m *GoalMachine) goalResume() DispatchResult {
	if m.view == nil || (m.view.Phase == PhaseActive && m.armed) {
		return DispatchResult{Reply: stringPtr("No stopped goal to resume."), IsError: true}
	}
	if stopReason := budgetStopReason(m.view, m.now().UnixMilli()); stopReason != nil {
		return DispatchResult{Reply: stringPtr(stopReason.Message + " Start a new goal with a larger budget."), IsError: true}
	}
	next := m.view.GoalSnapshot
	next.Phase = PhaseActive
	next.BlockedReason = nil
	next.Revision = m.view.Revision + 1
	next.UpdatedAt = m.now().UnixMilli()
	m.armed = true
	m.pendingTurn = nil
	effects, err := m.commit(OpResume, &next, nil)
	if err != nil {
		return DispatchResult{Reply: stringPtr(err.Error()), IsError: true}
	}
	return DispatchResult{Effects: append(effects, m.queueRound()...)}
}

func (m *GoalMachine) agentEnd(_ ContextUsage, usage *GoalUsage, aborted bool) DispatchResult {
	var effects []Effect

	if m.view == nil {
		m.pendingTurn = nil
		m.createdThisRun = false
		return DispatchResult{Effects: []Effect{}}
	}
	// Completed goals admit no rounds — a finishing run is not work done "for"
	// the goal, and the round card after the completion card is noise.
	if m.view.Phase == PhaseComplete {
		m.pendingTurn = nil
		m.createdThisRun = false
		return DispatchResult{Effects: effects}
	}

	// Was this run a goal attempt? Decides how cancellation is handled.
	wasGoalAttempt := m.pendingTurn != nil || m.createdThisRun

	if wasGoalAttempt {
		var usageValue GoalUsage
		if usage != nil {
			usageValue = *usage
		}
		effects = append(effects, AppendEntryEffect{EntryType: GoalTurnType, Data: GoalTurnEntry{
			GoalID:    m.view.ID,
			Revision:  m.view.Revision,
			Turn:      m.view.TurnsStarted + 1,
			Timestamp: m.now().UnixMilli(),
			Usage:     &usageValue,
		}})
		m.view.TurnsStarted = m.view.TurnsStarted + 1
		m.view.UsedTokens += maxInt64(0, usageValue.Tokens)
		m.view.UsedCostUsd += math.Max(0, usageValue.CostUsd)
		m.createdThisRun = false
		m.pendingTurn = nil
	}

	if m.view.Phase != PhaseActive {
		return DispatchResult{Effects: effects}
	}

	if aborted {
		if wasGoalAttempt {
			next := m.view.GoalSnapshot
			next.Phase = PhasePaused
			next.BlockedReason = &BlockedReason{Code: "cancelled", Message: "Goal round was cancelled."}
			next.Revision = m.view.Revision + 1
			next.UpdatedAt = m.now().UnixMilli()
			m.armed = false
			committed, err := m.commit(OpPause, &next, nil)
			if err == nil {
				effects = append(effects, committed...)
			}
		} else {
			m.armed = false
		}
		return DispatchResult{Effects: effects}
	}

	return DispatchResult{Effects: effects}
}

func (m *GoalMachine) agentBeforeSettle(outcome string, hasPendingMessages bool, now int64) DispatchResult {
	if m.view == nil || m.view.Phase != PhaseActive || !m.armed {
		return DispatchResult{Effects: []Effect{}, Continue: false}
	}
	if outcome == "aborted" {
		m.armed = false
		next := m.view.GoalSnapshot
		next.Phase = PhasePaused
		next.BlockedReason = &BlockedReason{Code: "cancelled", Message: "Goal round was cancelled."}
		next.Revision = m.view.Revision + 1
		next.UpdatedAt = now
		effects, err := m.commit(OpPause, &next, nil)
		if err != nil {
			return DispatchResult{Effects: []Effect{}, Continue: false}
		}
		return DispatchResult{Effects: effects, Continue: false}
	}
	if hasPendingMessages {
		return DispatchResult{Effects: []Effect{}, Continue: false}
	}
	if outcome == "error" {
		m.armed = false
		next := m.view.GoalSnapshot
		next.Phase = PhasePaused
		next.BlockedReason = &BlockedReason{Code: "run-error", Message: "Agent run ended with an error."}
		next.Revision = m.view.Revision + 1
		next.UpdatedAt = now
		effects, err := m.commit(OpPause, &next, nil)
		if err != nil {
			return DispatchResult{Effects: []Effect{}, Continue: false}
		}
		return DispatchResult{Effects: effects, Continue: false}
	}
	if reason := budgetStopReason(m.view, now); reason != nil {
		m.armed = false
		next := m.view.GoalSnapshot
		next.Phase = PhasePaused
		next.BlockedReason = reason
		next.Revision = m.view.Revision + 1
		next.UpdatedAt = now
		effects, err := m.commit(OpPause, &next, nil)
		if err != nil {
			return DispatchResult{Effects: []Effect{}, Continue: false}
		}
		effects = append(effects, NotifyEffect{Message: "Goal paused: " + reason.Message, Level: "warning"})
		return DispatchResult{Effects: effects, Continue: false}
	}
	turn := m.view.TurnsStarted + 1
	m.pendingTurn = &turn
	return DispatchResult{
		Effects:  []Effect{ContinueRoundEffect{Content: GoalRoundPrompt(*m.view, turn, time.UnixMilli(now)), Turn: turn}},
		Continue: true,
	}
}

func (m *GoalMachine) goalPause() DispatchResult {
	if m.view == nil || m.view.Phase != PhaseActive {
		return DispatchResult{Reply: stringPtr("No active goal."), IsError: true}
	}
	m.armed = false
	next := m.view.GoalSnapshot
	next.Phase = PhasePaused
	next.BlockedReason = &BlockedReason{Code: "human-paused", Message: "Paused by user."}
	next.Revision = m.view.Revision + 1
	next.UpdatedAt = m.now().UnixMilli()
	effects, err := m.commit(OpPause, &next, nil)
	if err != nil {
		return DispatchResult{Reply: stringPtr(err.Error()), IsError: true}
	}
	return DispatchResult{Effects: effects}
}

func (m *GoalMachine) goalClear(id string, revision int) DispatchResult {
	if m.view == nil {
		return DispatchResult{Reply: stringPtr("No goal is set."), IsError: true}
	}
	if id != m.view.ID {
		return DispatchResult{Reply: stringPtr("clear of unknown goal"), IsError: true}
	}
	if revision != m.view.Revision {
		return DispatchResult{Reply: stringPtr(fmt.Sprintf("stale clear: expected revision %d", m.view.Revision)), IsError: true}
	}
	effects, err := m.commit(OpClear, nil, &ClearedRef{ID: id, Revision: revision})
	if err != nil {
		return DispatchResult{Reply: stringPtr(err.Error()), IsError: true}
	}
	return DispatchResult{Effects: effects}
}

func (m *GoalMachine) goalSet(objective string, budget *GoalBudget) DispatchResult {
	if m.view != nil && m.view.Phase != PhaseComplete {
		return DispatchResult{Reply: stringPtr("An unfinished goal exists. /goal clear first (or /goal edit once implemented)."), IsError: true}
	}
	next := CreateGoalState(objective, m.now(), budget)
	m.armed = true
	m.pendingTurn = nil
	effects, err := m.commit(OpCreate, &next, nil)
	if err != nil {
		return DispatchResult{Reply: stringPtr(err.Error()), IsError: true}
	}
	return DispatchResult{Effects: append(effects, m.queueRound()...)}
}

// goalReplace replaces a goal the human already confirmed replacing. The old
// goal is tombstoned rather than overwritten so replay still validates.
func (m *GoalMachine) goalReplace(objective string, budget *GoalBudget) DispatchResult {
	// A completed goal is terminal: applyChange accepts create over it.
	if m.view == nil || m.view.Phase == PhaseComplete {
		return m.goalSet(objective, budget)
	}
	cleared, err := m.commit(OpClear, nil, &ClearedRef{ID: m.view.ID, Revision: m.view.Revision})
	if err != nil {
		return DispatchResult{Reply: stringPtr(err.Error()), IsError: true}
	}
	next := CreateGoalState(objective, m.now(), budget)
	m.armed = true
	m.pendingTurn = nil
	created, err := m.commit(OpCreate, &next, nil)
	if err != nil {
		return DispatchResult{Reply: stringPtr(err.Error()), IsError: true}
	}
	return DispatchResult{Effects: append(append(cleared, created...), m.queueRound()...)}
}

func (m *GoalMachine) sessionStart(entries []CustomEntry, reason string) DispatchResult {
	filtered := make([]CustomEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.CustomType == GoalCustomType || entry.CustomType == GoalTurnType {
			filtered = append(filtered, entry)
		}
	}
	folded, err := FoldGoal(filtered)
	if err != nil {
		// Surface corruption: never silently drop the goal.
		m.view = nil
		m.armed = false
		m.pendingTurn = nil
		m.createdThisRun = false
		return DispatchResult{Effects: []Effect{NotifyEffect{
			Message: "Goal state corrupt, ignoring: " + err.Error(),
			Level:   "warning",
		}}}
	}
	m.view = folded.Goal
	// Activation is never inherited: reload, resume, fork, and startup disarm.
	m.armed = false
	m.pendingTurn = nil
	m.createdThisRun = false

	// A reload is not a fresh start. Persist the stop so the paused goal is
	// visible in the transcript and on the next command.
	if reason == "reload" && m.view != nil && m.view.Phase == PhaseActive {
		objective := TruncateObjective(m.view.Objective, 60)
		next := m.view.GoalSnapshot
		next.Phase = PhasePaused
		next.BlockedReason = &BlockedReason{Code: "reloaded", Message: "Pi reloaded; the goal did not resume."}
		next.Revision = m.view.Revision + 1
		next.UpdatedAt = m.now().UnixMilli()
		effects, commitErr := m.commit(OpPause, &next, nil)
		if commitErr != nil {
			return DispatchResult{Effects: []Effect{
				NotifyEffect{Message: "Goal not paused after reload: " + commitErr.Error(), Level: "warning"},
			}}
		}
		effects = append(effects, NotifyEffect{Message: "Goal paused after reload: " + objective + "\nUse /goal resume to continue.", Level: "info"})
		return DispatchResult{Effects: effects}
	}

	return DispatchResult{Effects: []Effect{}}
}

func stringPtr(value string) *string { return &value }

func coerceRevision(raw any) (int, bool) {
	switch typed := raw.(type) {
	case nil:
		return 0, false
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, false
		}
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}
