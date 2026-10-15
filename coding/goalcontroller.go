package coding

// Port of the goal extension's index.ts host glue: the GoalController applies
// the GoalMachine's effects against the session (durable custom entries,
// continuation messages and notifications), exposes the three tools, and owns
// the lifecycle hooks.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
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
	// GoalActiveTools lists the active tool names.
	GoalActiveTools() []string
	// SetGoalActiveTools replaces the active tool names.
	SetGoalActiveTools(names []string)
	// GoalHasPendingMessages reports queued user input awaiting delivery.
	GoalHasPendingMessages() bool
	// ConfirmGoalReplace asks the human before replacing a live goal and invokes
	// onAnswer with their decision (asynchronously, like the host dialog).
	ConfirmGoalReplace(title string, message string, onAnswer func(confirmed bool))
}

// GoalController owns the goal machine and its host wiring.
type GoalController struct {
	mu      sync.Mutex
	machine *goal.GoalMachine
	sink    GoalSink
}

// NewGoalController creates a controller.
func NewGoalController(machine *goal.GoalMachine, sink GoalSink) *GoalController {
	if machine == nil {
		machine = goal.NewGoalMachine()
	}
	return &GoalController{machine: machine, sink: sink}
}

// Snapshot exposes the machine state.
func (c *GoalController) Snapshot() goal.MachineSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.machine.Snapshot()
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
		case goal.ContinueRoundEffect:
			// Handled by AgentEnd, which queues the hidden follow-up round.
		}
	}
	// Tool exposure mirrors the goal phase, and only a state mutation moves it.
	if mutated {
		c.SyncTools()
	}
}

// SyncTools mirrors the goal phase onto the active tool set.
func (c *GoalController) SyncTools() {
	current := c.sink.GoalActiveTools()
	seen := map[string]bool{}
	next := make([]string, 0, len(current)+3)
	for _, name := range current {
		if !seen[name] {
			next = append(next, name)
			seen[name] = true
		}
	}
	// create_goal stays available: the model may be asked to set a goal.
	if !seen["create_goal"] {
		next = append(next, "create_goal")
		seen["create_goal"] = true
	}
	// get_goal/update_goal only mean something while a goal is being pursued.
	c.mu.Lock()
	pursuing := false
	if snapshot := c.machine.Snapshot(); snapshot.Goal != nil {
		pursuing = snapshot.Goal.Phase == goal.PhaseActive
	}
	c.mu.Unlock()
	for _, name := range []string{"get_goal", "update_goal"} {
		if pursuing {
			if !seen[name] {
				next = append(next, name)
				seen[name] = true
			}
			continue
		}
		delete(seen, name)
	}
	if !pursuing {
		filtered := next[:0]
		for _, name := range next {
			if name != "get_goal" && name != "update_goal" {
				filtered = append(filtered, name)
			}
		}
		next = filtered
	}
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

// SessionStart replays durable entries and applies the reload pause.
func (c *GoalController) SessionStart(reason string, entries []goal.CustomEntry) {
	c.mu.Lock()
	result := c.machine.Dispatch(goal.SessionStartEvent{Entries: entries, Reason: reason})
	c.mu.Unlock()
	c.Apply(result.Effects)
	c.SyncTools()
	snapshot := c.machine.Snapshot()
	if snapshot.Goal != nil && snapshot.Goal.Phase == goal.PhaseActive && reason != "reload" {
		c.sink.NotifyGoal("Goal restored (disarmed): "+goal.TruncateObjective(snapshot.Goal.Objective, 60)+"\nUse /goal resume to continue.", "info")
	}
}

// AgentEnd admits the creating run or the reserved round, then decides whether
// the next round continues (agent_end + agent_before_settle).
func (c *GoalController) AgentEnd(event *SessionEvent) {
	aborted := goalAborted(event)
	c.mu.Lock()
	admission := c.machine.Dispatch(goal.AgentEndEvent{Usage: goalUsageFromAgentEnd(event), Aborted: aborted})
	c.mu.Unlock()
	c.Apply(admission.Effects)
	pending := c.sink.GoalHasPendingMessages()
	c.mu.Lock()
	settle := c.machine.Dispatch(goal.AgentBeforeSettleEvent{Outcome: goalOutcome(event, aborted), HasPendingMessages: pending})
	c.mu.Unlock()
	c.Apply(settle.Effects)
	if !settle.Continue {
		return
	}
	for _, effect := range settle.Effects {
		if round, ok := effect.(goal.ContinueRoundEffect); ok {
			details, _ := json.Marshal(map[string]any{"kind": "round", "turn": round.Turn})
			c.sink.SendGoalMessage(goal.GoalEventType, round.Content, false, details, true)
			return
		}
	}
}

// goalUsageFromAgentEnd sums the run's assistant usage.
func goalUsageFromAgentEnd(event *SessionEvent) *goal.GoalUsage {
	if event == nil || event.Agent == nil {
		return nil
	}
	usage := goal.GoalUsage{}
	for _, message := range event.Agent.Messages {
		if assistant, ok := message.(*ai.AssistantMessage); ok {
			if assistant.Usage.TotalTokens > 0 {
				usage.Tokens += assistant.Usage.TotalTokens
			}
			if assistant.Usage.Cost.Total > 0 {
				usage.CostUsd += assistant.Usage.Cost.Total
			}
		}
	}
	return &usage
}

// goalAborted reports whether the run that ended was cancelled.
func goalAborted(event *SessionEvent) bool {
	if event == nil || event.Agent == nil {
		return false
	}
	for index := len(event.Agent.Messages) - 1; index >= 0; index-- {
		if assistant, ok := event.Agent.Messages[index].(*ai.AssistantMessage); ok {
			return assistant.StopReason == ai.StopAborted
		}
	}
	return false
}

// goalOutcome classifies the settle: aborted, error, or completed.
func goalOutcome(event *SessionEvent, aborted bool) string {
	if aborted {
		return "aborted"
	}
	if event != nil && event.Agent != nil {
		for index := len(event.Agent.Messages) - 1; index >= 0; index-- {
			if assistant, ok := event.Agent.Messages[index].(*ai.AssistantMessage); ok {
				if assistant.StopReason == ai.StopError {
					return "error"
				}
				return "completed"
			}
		}
	}
	return "completed"
}

// HandleCommand runs the /goal command.
func (c *GoalController) HandleCommand(args string) {
	c.mu.Lock()
	snapshot := c.machine.Snapshot()
	c.mu.Unlock()
	run := func(event goal.GoalEvent, fallback func() string) {
		c.mu.Lock()
		result := c.machine.Dispatch(event)
		c.mu.Unlock()
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
	case goal.CmdShowStatus:
		c.sink.NotifyGoal(goal.GoalStatusMessage(snapshot.Goal, time.Now()), "info")
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
			view := c.Snapshot().Goal
			payload, err := ai.MarshalIndentJSON(goal.GoalViewOf(view))
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			details, _ := json.Marshal(map[string]any{"goal": view})
			return agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: string(payload)}},
				Details: details,
			}, nil
		},
	}
}

func (c *GoalController) createGoalTool() agent.AgentTool {
	return agent.AgentTool{
		Name:        "create_goal",
		Label:       "Create Goal",
		Description: "Create a persisted session goal that continues through Pi's default unbudgeted round loop. Not for trivial single-turn work.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"objective":{"type":"string","description":"Concrete completion objective."}},"required":["objective"],"additionalProperties":false}`),
		Execute: func(_ string, params json.RawMessage, _ context.Context, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			var input struct {
				Objective string `json:"objective"`
			}
			if err := json.Unmarshal(params, &input); err != nil {
				return goalErrorResult("objective is required.")
			}
			objective := strings.TrimSpace(input.Objective)
			if objective == "" {
				return goalErrorResult("objective is required.")
			}
			c.mu.Lock()
			result := c.machine.Dispatch(goal.GoalCreateEvent{Objective: objective})
			c.mu.Unlock()
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
				return goalErrorResult(err.Error())
			}
			var revision any
			if len(input.Revision) > 0 {
				_ = json.Unmarshal(input.Revision, &revision)
			}
			c.mu.Lock()
			result := c.machine.Dispatch(goal.GoalUpdateEvent{
				GoalID: input.GoalID, Revision: revision, Action: input.Action, BlockedReason: input.BlockedReason,
			})
			c.mu.Unlock()
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
	details, _ := json.Marshal(map[string]any{"goal": c.Snapshot().Goal})
	return agent.AgentToolResult{
		Content: []ai.Content{ai.TextContent{Text: reply}},
		Details: details,
		IsError: result.IsError,
	}, nil
}

func goalErrorResult(message string) (agent.AgentToolResult, error) {
	return agent.AgentToolResult{
		Content: []ai.Content{ai.TextContent{Text: message}},
		IsError: true,
	}, nil
}

// SessionGoalSink adapts an AgentSession to GoalSink. The interactive mode sets
// Notify and Confirm; the rest is session-backed.
type SessionGoalSink struct {
	session *AgentSession
	Notify  func(message string, level string)
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
	s.session.AppendCustomMessage(goalEventMessage(customType, content, display, details))
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
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
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

// GoalHasPendingMessages reports queued user input.
func (s *SessionGoalSink) GoalHasPendingMessages() bool {
	if s.session == nil {
		return false
	}
	return s.session.PendingMessageCount() > 0
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
// manager's current branch, mirroring the extension's session_start hook.
func (c *GoalController) SessionStartFromSession(reason string, manager *SessionManager) {
	var entries []goal.CustomEntry
	if manager != nil {
		for _, entry := range manager.GetBranch("") {
			if entry.Type != "custom" {
				continue
			}
			entries = append(entries, goal.CustomEntry{CustomType: entry.CustomType, Data: entry.Data})
		}
	}
	c.SessionStart(reason, entries)
}

// GoalToolNames are the goal tool names in registration order.
var GoalToolNames = []string{"get_goal", "create_goal", "update_goal"}
