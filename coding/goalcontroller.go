package coding

// Port of the goal extension's index.ts host glue: the GoalController applies
// the GoalMachine's effects against the session (durable custom entries,
// continuation messages, notifications, the retry timer and the status
// widget), exposes the three tools, and owns the lifecycle hooks.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/goal"
)

// GoalSink is the host surface the goal controller needs. The interactive mode
// (and the tests) implement it.
type GoalSink interface {
	// AppendGoalEntry persists a durable custom entry.
	AppendGoalEntry(entryType string, data json.RawMessage)
	// SendGoalMessage adds a continuation or wrap-up message to the model
	// context, optionally starting a turn.
	SendGoalMessage(customType string, content string, display bool, details json.RawMessage, triggerTurn bool)
	// NotifyGoal surfaces a notification to the human.
	NotifyGoal(message string, level string)
	// RenderGoalStatus re-renders the status widget.
	RenderGoalStatus()
	// GoalActiveTools lists the active tool names.
	GoalActiveTools() []string
	// SetGoalActiveTools replaces the active tool names.
	SetGoalActiveTools(names []string)
	// ConfirmGoalReplace asks the human before replacing a live goal and invokes
	// onAnswer with their decision (asynchronously, like the host dialog).
	ConfirmGoalReplace(title string, message string, onAnswer func(confirmed bool))
}

// GoalController owns the goal machine and its host wiring.
type GoalController struct {
	machine *goal.GoalMachine
	sink    GoalSink

	mu            sync.Mutex
	providerError *goal.ProviderError
	retryTimer    *time.Timer
}

// NewGoalController creates a controller.
func NewGoalController(machine *goal.GoalMachine, sink GoalSink) *GoalController {
	if machine == nil {
		machine = goal.NewGoalMachine()
	}
	return &GoalController{machine: machine, sink: sink}
}

// Machine exposes the underlying state machine.
func (c *GoalController) Machine() *goal.GoalMachine { return c.machine }

// Apply executes the machine's effects against the host.
func (c *GoalController) Apply(effects []goal.Effect) {
	mutated := false
	for _, effect := range effects {
		switch typed := effect.(type) {
		case goal.AppendEntryEffect:
			data, err := json.Marshal(typed.Data)
			if err != nil {
				continue
			}
			c.sink.AppendGoalEntry(typed.EntryType, data)
			mutated = true
		case goal.SendMessageEffect:
			details, _ := json.Marshal(typed.Details)
			c.sink.SendGoalMessage(typed.CustomType, typed.Content, typed.Display, details, typed.TriggerTurn)
		case goal.NotifyEffect:
			c.sink.NotifyGoal(typed.Message, typed.Level)
		case goal.ScheduleRetryEffect:
			c.scheduleRetry(typed)
		case goal.RenderStatusEffect:
			c.sink.RenderGoalStatus()
		}
	}
	// Tool exposure mirrors the goal phase, and only a state mutation moves it.
	if mutated {
		c.SyncTools()
	}
}

func (c *GoalController) scheduleRetry(effect goal.ScheduleRetryEffect) {
	seconds := effect.DelayMs / 1000
	c.sink.NotifyGoal(fmt.Sprintf("%s — retrying in %ds (attempt %d/%d).", effect.Error, seconds, effect.Attempt, effect.MaxRetries), "warning")
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.retryTimer != nil {
		c.retryTimer.Stop()
	}
	c.retryTimer = time.AfterFunc(time.Duration(effect.DelayMs)*time.Millisecond, func() {
		// No host context: timer callbacks outlive the settling context; a
		// retry_due effect only sends a message and renders status.
		c.Apply(c.machine.Dispatch(goal.RetryDueEvent{}).Effects)
	})
}

// SyncTools mirrors the goal phase onto the active tool set.
func (c *GoalController) SyncTools() {
	desired := map[string]bool{}
	for _, name := range c.sink.GoalActiveTools() {
		desired[name] = true
	}
	// create_goal stays available: the model may be asked to set a goal.
	desired["create_goal"] = true
	// get_goal/update_goal only mean something while a goal is being pursued.
	snapshot := c.machine.Snapshot()
	pursuing := snapshot.Goal != nil && snapshot.Goal.Phase == goal.PhaseActive
	for _, name := range []string{"get_goal", "update_goal"} {
		if pursuing {
			desired[name] = true
		} else {
			delete(desired, name)
		}
	}
	next := make([]string, 0, len(desired))
	for name := range desired {
		next = append(next, name)
	}
	sort.Strings(next)
	current := c.sink.GoalActiveTools()
	if !sameToolSet(next, current) {
		c.sink.SetGoalActiveTools(next)
	}
}

func sameToolSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, name := range a {
		set[name] = true
	}
	for _, name := range b {
		if !set[name] {
			return false
		}
	}
	return true
}

// ProviderResponse records the last failing provider HTTP response.
func (c *GoalController) ProviderResponse(status int, retryAfterMs *int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if status >= 400 {
		c.providerError = &goal.ProviderError{Status: status, Message: fmt.Sprintf("HTTP %d", status), RetryAfterMs: retryAfterMs}
		return
	}
	c.providerError = nil
}

// AgentStart clears the run's provider error and retry timer.
func (c *GoalController) AgentStart() {
	c.mu.Lock()
	c.providerError = nil
	if c.retryTimer != nil {
		c.retryTimer.Stop()
		c.retryTimer = nil
	}
	c.mu.Unlock()
}

// SessionStart replays durable entries and applies the reload pause.
func (c *GoalController) SessionStart(reason string, entries []goal.CustomEntry) {
	result := c.machine.Dispatch(goal.SessionStartEvent{Entries: entries, Reason: reason})
	c.Apply(result.Effects)
	c.SyncTools()
	c.sink.RenderGoalStatus()
	snapshot := c.machine.Snapshot()
	if snapshot.Goal != nil && snapshot.Goal.Phase == goal.PhaseActive && reason != "reload" {
		c.sink.NotifyGoal("Goal restored (disarmed): "+goal.TruncateObjective(snapshot.Goal.Objective, 60)+"\nUse /goal resume to continue.", "info")
	}
}

// AgentEnd admits the creating run or the reserved round, and pauses on a
// cancellation.
func (c *GoalController) AgentEnd(aborted bool) {
	c.Apply(c.machine.Dispatch(goal.AgentEndEvent{Aborted: aborted}).Effects)
}

// AgentSettled queues the next round, retries, or pauses on a provider error.
func (c *GoalController) AgentSettled(hasPendingMessages bool) {
	c.mu.Lock()
	providerError := c.providerError
	c.mu.Unlock()
	result := c.machine.Dispatch(goal.AgentSettledEvent{ProviderError: providerError, HasPendingMessages: hasPendingMessages})
	c.Apply(result.Effects)
	snapshot := c.machine.Snapshot()
	if providerError != nil && snapshot.Goal != nil && snapshot.Goal.Phase == goal.PhasePaused &&
		snapshot.Goal.BlockedReason != nil && strings.HasPrefix(snapshot.Goal.BlockedReason.Code, "api-") {
		c.sink.NotifyGoal("Goal paused: "+snapshot.Goal.BlockedReason.Message+" "+goal.ResumeHint(*snapshot.Goal.BlockedReason), "warning")
	}
}

// HandleCommand runs the /goal command.
func (c *GoalController) HandleCommand(args string) {
	snapshot := c.machine.Snapshot()
	run := func(event goal.GoalEvent, fallback func() string) {
		result := c.machine.Dispatch(event)
		c.Apply(result.Effects)
		if result.Reply != nil {
			level := "info"
			if result.IsError {
				level = "warning"
			}
			c.sink.NotifyGoal(*result.Reply, level)
			return
		}
		if !result.IsError && fallback != nil {
			if text := fallback(); text != "" {
				c.sink.NotifyGoal(text, "info")
			}
		}
	}

	switch command := goal.ParseGoalCommand(args); command.Kind {
	case goal.CmdToggleBanner:
		run(goal.BannerToggleEvent{}, func() string {
			if c.machine.Snapshot().BannerEnabled {
				return "Goal banner shown."
			}
			return "Goal banner hidden."
		})
	case goal.CmdShowStatus:
		c.sink.NotifyGoal(goal.GoalStatusMessage(snapshot.Goal, snapshot.BannerEnabled), "info")
	case goal.CmdClear:
		if snapshot.Goal == nil {
			c.sink.NotifyGoal("No goal is set.", "info")
			return
		}
		run(goal.GoalClearEvent{ID: snapshot.Goal.ID, Revision: snapshot.Goal.Revision}, func() string { return "Goal cleared." })
	case goal.CmdPause:
		run(goal.GoalPauseEvent{}, func() string { return "Goal paused." })
	case goal.CmdResume:
		run(goal.GoalResumeEvent{}, func() string { return "Goal resumed." })
	case goal.CmdSet:
		// Replacing a live goal is a destructive edit: confirm, then tombstone
		// the old one so replay still validates every step.
		if snapshot.Goal != nil && snapshot.Goal.Phase != goal.PhaseComplete {
			title := "Replace goal?"
			message := "Current: " + goal.TruncateObjective(snapshot.Goal.Objective, 120) + "\n\nNew: " + goal.TruncateObjective(command.Objective, 120)
			c.sink.ConfirmGoalReplace(title, message, func(confirmed bool) {
				if !confirmed {
					return
				}
				run(goal.GoalReplaceEvent{Objective: command.Objective}, func() string { return "Goal replaced." })
			})
			return
		}
		run(goal.GoalSetEvent{Objective: command.Objective}, func() string { return "Goal set." })
	case goal.CmdError:
		c.sink.NotifyGoal(command.Message, "warning")
	}
}

// goalToolGuidelines supplies the goal tools' prompt bullets.
var goalToolGuidelines = map[string][]string{
	"get_goal": {"Call get_goal before update_goal to copy the exact id and revision."},
	"create_goal": {
		"Use create_goal when the user's request is a multi-step objective that should continue across rounds.",
		"Do not create goals for trivial single-turn work.",
		"Before creating, turn the request into a concrete objective with outcome, verification, constraints, and boundaries.",
	},
	"update_goal": {
		"Call get_goal first to get the exact id and revision, unless this turn already gave you both.",
		"Mark complete only when the objective is actually achieved, with evidence.",
		"Mark blocked only after the same condition persisted for at least 3 consecutive goal rounds.",
	},
}

// GoalTools returns the three goal tools.
func (c *GoalController) GoalTools() []agent.AgentTool {
	return []agent.AgentTool{c.getGoalTool(), c.createGoalTool(), c.updateGoalTool()}
}

func (c *GoalController) getGoalTool() agent.AgentTool {
	return agent.AgentTool{
		Name:        "get_goal",
		Label:       "Get Goal",
		Description: "Read the current session goal. Call before update_goal for the exact id and revision.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Execute: func(_ string, _ json.RawMessage, _ context.Context, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			goalView := c.machine.Snapshot().Goal
			payload, err := json.Marshal(goal.GoalViewOf(goalView))
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			details := map[string]any{"goal": goalView}
			encodedDetails, _ := json.Marshal(details)
			return agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: string(payload)}},
				Details: encodedDetails,
			}, nil
		},
	}
}

func (c *GoalController) createGoalTool() agent.AgentTool {
	return agent.AgentTool{
		Name:        "create_goal",
		Label:       "Create Goal",
		Description: "Create a persisted session goal. Not for trivial single-turn work.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"objective":{"type":"string","description":"Concrete completion objective."}},"required":["objective"],"additionalProperties":false}`),
		Execute: func(_ string, params json.RawMessage, _ context.Context, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			var input struct {
				Objective string `json:"objective"`
			}
			if err := json.Unmarshal(params, &input); err != nil {
				return errorToolResult("objective is required.")
			}
			objective := strings.TrimSpace(input.Objective)
			if objective == "" {
				return errorToolResult("objective is required.")
			}
			result := c.machine.Dispatch(goal.GoalCreateEvent{Objective: objective})
			c.Apply(result.Effects)
			return c.toolResult(result, "Goal created.")
		},
	}
}

func (c *GoalController) updateGoalTool() agent.AgentTool {
	return agent.AgentTool{
		Name:        "update_goal",
		Label:       "Update Goal",
		Description: "Complete or block the session goal. Needs the exact id and revision from get_goal. complete: objective achieved with evidence. blocked: needs blocked_reason; rejected before 3 consecutive rounds. edit/pause/resume: human-only (/goal).",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"goal_id":{"type":"string","description":"Exact id from get_goal."},"revision":{"type":"number","description":"Exact revision from get_goal."},"action":{"type":"string","enum":["complete","blocked"],"description":"Action to perform."},"blocked_reason":{"type":"string","description":"Blocking condition (blocked only)."}},"required":["goal_id","revision","action"],"additionalProperties":false}`),
		Execute: func(_ string, params json.RawMessage, _ context.Context, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			var input struct {
				GoalID        string          `json:"goal_id"`
				Revision      json.RawMessage `json:"revision"`
				Action        string          `json:"action"`
				BlockedReason string          `json:"blocked_reason"`
			}
			if err := json.Unmarshal(params, &input); err != nil {
				return errorToolResult(err.Error())
			}
			var revision any
			if len(input.Revision) > 0 {
				_ = json.Unmarshal(input.Revision, &revision)
			}
			result := c.machine.Dispatch(goal.GoalUpdateEvent{
				GoalID: input.GoalID, Revision: revision, Action: input.Action, BlockedReason: input.BlockedReason,
			})
			c.Apply(result.Effects)
			return c.toolResult(result, "")
		},
	}
}

func (c *GoalController) toolResult(result goal.DispatchResult, fallback string) (agent.AgentToolResult, error) {
	reply := fallback
	if result.Reply != nil {
		reply = *result.Reply
	}
	if result.IsError {
		return errorToolResult(reply)
	}
	details, _ := json.Marshal(map[string]any{"goal": c.machine.Snapshot().Goal})
	return agent.AgentToolResult{
		Content: []ai.Content{ai.TextContent{Text: reply}},
		Details: details,
	}, nil
}

func errorToolResult(message string) (agent.AgentToolResult, error) {
	return agent.AgentToolResult{
		Content: []ai.Content{ai.TextContent{Text: message}},
	}, fmt.Errorf("%s", message)
}

// SessionGoalSink adapts an AgentSession to GoalSink. The interactive mode sets
// Notify/Render/Confirm; the rest is session-backed.
type SessionGoalSink struct {
	session *AgentSession
	Notify  func(message string, level string)
	Render  func()
	Confirm func(title string, message string, onAnswer func(confirmed bool))
}

// AppendGoalEntry persists a durable custom entry.
func (s *SessionGoalSink) AppendGoalEntry(entryType string, data json.RawMessage) {
	if s.session != nil && s.session.Sessions != nil {
		s.session.Sessions.AppendCustomEntry(entryType, data)
	}
}

// SendGoalMessage adds the continuation or wrap-up to the model context and,
// for a round, queues the follow-up turn.
func (s *SessionGoalSink) SendGoalMessage(customType string, content string, display bool, details json.RawMessage, triggerTurn bool) {
	if s.session == nil {
		return
	}
	if triggerTurn {
		// Upstream `sendMessage(..., { triggerTurn: true, deliverAs: "followUp" })`:
		// the round prompt enters the context as a display:false custom message
		// (invisible in the transcript) and the queued follow-up starts the turn.
		s.session.FollowUp(goalEventMessage(customType, content, display, details))
		return
	}
	encoded, _ := json.Marshal(content)
	s.session.AppendCustomMessage(&ai.CustomMessage{Role: customType, Content: encoded})
}

// goalEventMessage encodes a goal round/wrap-up as the extension's custom
// message: role "custom" with the {customType, content, display, details}
// envelope, so ConvertToLlm reaches the model and a display:false message stays
// out of the transcript.
func goalEventMessage(customType string, content string, display bool, details json.RawMessage) *ai.CustomMessage {
	contentJSON, err := json.Marshal(content)
	if err != nil {
		contentJSON = json.RawMessage(`""`)
	}
	fields, err := json.Marshal(customMessageFields{
		CustomType: customType,
		Content:    contentJSON,
		Display:    display,
		Details:    details,
	})
	if err != nil {
		fields = []byte(`{}`)
	}
	return &ai.CustomMessage{Role: RoleCustom, Content: fields}
}

// NotifyGoal surfaces a notification (no-op without a sink).
func (s *SessionGoalSink) NotifyGoal(message string, level string) {
	if s.Notify != nil {
		s.Notify(message, level)
	}
}

// RenderGoalStatus re-renders the status widget (no-op without a sink).
func (s *SessionGoalSink) RenderGoalStatus() {
	if s.Render != nil {
		s.Render()
	}
}

// GoalActiveTools lists the session's active tool names.
func (s *SessionGoalSink) GoalActiveTools() []string {
	if s.session == nil {
		return nil
	}
	return s.session.ActiveToolNames()
}

// SetGoalActiveTools replaces the session's active tool names.
func (s *SessionGoalSink) SetGoalActiveTools(names []string) {
	if s.session != nil {
		s.session.SetActiveToolsByName(names)
	}
}

// ConfirmGoalReplace asks the human (denies without a sink).
func (s *SessionGoalSink) ConfirmGoalReplace(title string, message string, onAnswer func(confirmed bool)) {
	if s.Confirm == nil {
		onAnswer(false)
		return
	}
	s.Confirm(title, message, onAnswer)
}

// AttachSession binds the controller to its session so its effects reach it.
func (c *GoalController) AttachSession(session *AgentSession) {
	if sink, ok := c.sink.(*SessionGoalSink); ok {
		sink.session = session
	}
}

// Goal returns the session's goal controller (nil when disabled).
func (s *AgentSession) Goal() *GoalController {
	if s.control == nil {
		return nil
	}
	return s.control.Goal
}

// SessionSink returns the session-backed sink when the controller uses one.
func (c *GoalController) SessionSink() *SessionGoalSink {
	if sink, ok := c.sink.(*SessionGoalSink); ok {
		return sink
	}
	return nil
}

// SessionStartFromSession replays the durable goal entries from a session
// manager, mirroring the extension's session_start hook.
func (c *GoalController) SessionStartFromSession(reason string, manager *SessionManager) {
	var entries []goal.CustomEntry
	if manager != nil {
		for _, entry := range manager.GetEntries() {
			if entry.Type != "custom" {
				continue
			}
			entries = append(entries, goal.CustomEntry{CustomType: entry.CustomType, Data: entry.Data})
		}
	}
	c.SessionStart(reason, entries)
}
