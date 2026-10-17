package coding

// The goal controller is a session feature: it owns the goal machine and
// applies its effects against the session it belongs to. It is built in
// NewAgentSession like the cache warmer, not passed in as an object graph from
// the SDK.

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

// goalSink is the session surface the goal controller needs. The production
// implementation wraps an AgentSession; tests use a fake.
type goalSink interface {
	// AppendGoalEntry persists a durable custom entry.
	AppendGoalEntry(entryType string, data json.RawMessage)
	// SendGoalMessage adds a continuation or wrap-up message to the model
	// context without starting a run; the caller checked that one is active.
	SendGoalMessage(customType string, content string, display bool, details json.RawMessage, triggerTurn bool)
	// RunGoalTurn delivers a trigger-turn goal message and starts a run when no
	// run is active (upstream sendCustomMessage's not-streaming branch).
	RunGoalTurn(ctx context.Context, customType string, content string, display bool, details json.RawMessage) error
	// GoalIsStreaming reports whether an agent run is active.
	GoalIsStreaming() bool
	// GoalActiveTools lists the active tool names.
	GoalActiveTools() []string
	// SetGoalActiveTools replaces the active tool names.
	SetGoalActiveTools(names []string)
	// GoalHasPendingMessages reports queued user input awaiting delivery.
	GoalHasPendingMessages() bool
	// GoalBranchEntries returns the current branch's custom entries for replay.
	GoalBranchEntries() []goal.CustomEntry
}

// sessionGoalSink adapts an AgentSession to goalSink.
type sessionGoalSink struct{ session *AgentSession }

func newSessionGoalSink(session *AgentSession) *sessionGoalSink {
	return &sessionGoalSink{session: session}
}

func (s *sessionGoalSink) AppendGoalEntry(entryType string, data json.RawMessage) {
	if s.session != nil && s.session.Sessions != nil {
		s.session.Sessions.AppendCustomEntry(entryType, data)
	}
}

func (s *sessionGoalSink) SendGoalMessage(customType string, content string, display bool, details json.RawMessage, triggerTurn bool) {
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

func (s *sessionGoalSink) GoalIsStreaming() bool {
	return s.session != nil && s.session.IsStreaming()
}

// RunGoalTurn is upstream sendCustomMessage's not-streaming + triggerTurn
// branch: it starts a run with the custom message (deliverAs only matters while
// streaming).
func (s *sessionGoalSink) RunGoalTurn(ctx context.Context, customType string, content string, display bool, details json.RawMessage) error {
	if s.session == nil {
		return nil
	}
	return s.session.SendMessage(ctx, goalEventMessage(customType, content, display, details), &SendMessageOptions{DeliverAs: "followUp"})
}

func (s *sessionGoalSink) GoalActiveTools() []string {
	if s.session == nil {
		return nil
	}
	return s.session.ActiveToolNames()
}

func (s *sessionGoalSink) SetGoalActiveTools(names []string) {
	if s.session != nil {
		s.session.SetActiveToolsByName(names)
	}
}

func (s *sessionGoalSink) GoalHasPendingMessages() bool {
	return s.session != nil && s.session.PendingMessageCount() > 0
}

func (s *sessionGoalSink) GoalBranchEntries() []goal.CustomEntry {
	if s.session == nil || s.session.Sessions == nil {
		return nil
	}
	var entries []goal.CustomEntry
	for _, entry := range s.session.Sessions.GetBranch("") {
		if entry.Type != "custom" {
			continue
		}
		entries = append(entries, goal.CustomEntry{CustomType: entry.CustomType, Data: entry.Data})
	}
	return entries
}

// goalNotice is a notification buffered until the UI hooks are installed.
type goalNotice struct {
	message string
	level   string
}

// GoalController owns the goal machine and its session wiring.
type GoalController struct {
	mu      sync.Mutex
	machine *goal.GoalMachine
	sink    goalSink
	notify  func(message string, level string)
	// startWork schedules a run on the host's work slot when the session is
	// idle. A nil scheduler runs the delivery inline (SDK and print mode).
	startWork func(fn func(context.Context) error)
	pending   []goalNotice
}

// newGoalController creates a controller bound to a sink.
func newGoalController(machine *goal.GoalMachine, sink goalSink) *GoalController {
	if machine == nil {
		machine = goal.NewGoalMachine()
	}
	return &GoalController{machine: machine, sink: sink}
}

// SetUIHooks installs the human-facing callback and delivers any notices that
// were produced before the UI existed (session-start restore, reload pauses).
// A nil notify drops notices.
func (c *GoalController) SetUIHooks(notify func(message string, level string)) {
	c.mu.Lock()
	c.notify = notify
	notices := c.pending
	c.pending = nil
	c.mu.Unlock()
	if notify == nil {
		return
	}
	for _, notice := range notices {
		notify(notice.message, notice.level)
	}
}

// notice surfaces a notification, buffering it until hooks are installed.
func (c *GoalController) notice(message string, level string) {
	c.mu.Lock()
	if c.notify == nil {
		c.pending = append(c.pending, goalNotice{message: message, level: level})
		c.mu.Unlock()
		return
	}
	notify := c.notify
	c.mu.Unlock()
	notify(message, level)
}

// SetStartWork installs the host's off-loop work scheduler. When an idle goal
// round is ready, the controller uses it so a resumed goal starts a run without
// blocking the caller (the interactive loop's single work slot).
func (c *GoalController) SetStartWork(fn func(fn func(context.Context) error)) {
	c.mu.Lock()
	c.startWork = fn
	c.mu.Unlock()
}

// StartWorkWired reports whether a work scheduler is installed. The interactive
// wiring uses it to check that a session rebind rewired the launcher.
func (c *GoalController) StartWorkWired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startWork != nil
}

// deliverGoalMessage ports sendCustomMessage's triggerTurn routing: while a run
// is active the message queues behind it; otherwise a run starts. The two
// trigger-turn round sources (a /goal command and agent_before_settle) share
// this path so a round transition is the same either way.
func (c *GoalController) deliverGoalMessage(customType string, content string, display bool, details json.RawMessage, triggerTurn bool) {
	if !triggerTurn {
		c.sink.SendGoalMessage(customType, content, display, details, false)
		return
	}
	if c.sink.GoalIsStreaming() {
		c.sink.SendGoalMessage(customType, content, display, details, true)
		return
	}
	deliver := func(ctx context.Context) error {
		return c.sink.RunGoalTurn(ctx, customType, content, display, details)
	}
	c.mu.Lock()
	startWork := c.startWork
	c.mu.Unlock()
	if startWork != nil {
		startWork(deliver)
		return
	}
	_ = deliver(context.Background())
}

// Snapshot exposes the machine state.
func (c *GoalController) Snapshot() goal.MachineSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.machine.Snapshot()
}

func goalRoundDetails(goalID string, turn int) map[string]any {
	return map[string]any{"kind": "round", "turn": turn, "goalId": goalID}
}

// roundPromptIfMissing restores the reserved round's hidden prompt when
// compaction removed it from the live context. D224: upstream queues this
// prompt once at agent_before_settle, so a mid-round compaction can otherwise
// remove it before the current round settles.
func (c *GoalController) roundPromptIfMissing(messages []ai.Message) *ai.CustomMessage {
	snapshot := c.Snapshot()
	if snapshot.Goal == nil || snapshot.Goal.Phase != goal.PhaseActive || !snapshot.Goal.Armed || snapshot.PendingTurn == nil {
		return nil
	}
	turn := *snapshot.PendingTurn
	for _, message := range messages {
		custom, ok := message.(*ai.CustomMessage)
		if !ok || custom.Role != RoleCustom {
			continue
		}
		var fields customMessageFields
		if json.Unmarshal(custom.Content, &fields) != nil || fields.CustomType != goal.GoalEventType {
			continue
		}
		var details struct {
			Kind   string `json:"kind"`
			Turn   int    `json:"turn"`
			GoalID string `json:"goalId"`
		}
		if json.Unmarshal(fields.Details, &details) == nil && details.Kind == "round" &&
			details.Turn == turn && details.GoalID == snapshot.Goal.ID {
			return nil
		}
	}
	content := goal.GoalRoundPrompt(*snapshot.Goal, turn, time.Now())
	details, _ := json.Marshal(goalRoundDetails(snapshot.Goal.ID, turn))
	return goalEventMessage(goal.GoalEventType, content, false, details)
}

// Apply executes the machine's effects against the session.
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
			detailValues := typed.Details
			if typed.TriggerTurn && typed.CustomType == goal.GoalEventType && typed.Details["kind"] == "round" {
				if turn, ok := typed.Details["turn"].(int); ok {
					if snapshot := c.Snapshot().Goal; snapshot != nil {
						detailValues = goalRoundDetails(snapshot.ID, turn)
					}
				}
			}
			details, _ := json.Marshal(detailValues)
			c.deliverGoalMessage(typed.CustomType, typed.Content, typed.Display, details, typed.TriggerTurn)
		case goal.NotifyEffect:
			c.notice(typed.Message, typed.Level)
		case goal.ContinueRoundEffect:
			// The agent_before_settle round. Delivering it through the same path
			// as a /goal round keeps the transition identical: it follows up behind
			// the active run (agent_end runs while the run is still active).
			detailValues := map[string]any{"kind": "round", "turn": typed.Turn}
			if snapshot := c.Snapshot().Goal; snapshot != nil {
				detailValues = goalRoundDetails(snapshot.ID, typed.Turn)
			}
			details, _ := json.Marshal(detailValues)
			c.deliverGoalMessage(goal.GoalEventType, typed.Content, false, details, true)
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
	c.mu.Lock()
	pursuing := false
	if snapshot := c.machine.Snapshot(); snapshot.Goal != nil {
		pursuing = snapshot.Goal.Phase == goal.PhaseActive
	}
	c.mu.Unlock()
	next := goal.DesiredToolNames(current, pursuing)
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

// Start replays the branch's durable goal entries. "reload" additionally
// durably pauses a live goal.
func (c *GoalController) Start(reason string) {
	c.mu.Lock()
	result := c.machine.Dispatch(goal.SessionStartEvent{Entries: c.sink.GoalBranchEntries(), Reason: reason})
	c.mu.Unlock()
	c.Apply(result.Effects)
	c.SyncTools()
	snapshot := c.Snapshot()
	if snapshot.Goal != nil && snapshot.Goal.Phase == goal.PhaseActive && reason != "reload" {
		c.notice("Goal restored (disarmed): "+goal.TruncateObjective(snapshot.Goal.Objective, 60)+"\nUse /goal resume to continue.", "info")
	}
}

// AgentEnd admits the creating run or the reserved round and decides whether
// the next round continues (the agent_end + agent_before_settle hooks). Both
// effect sets go through Apply, so the continuation round is delivered the same
// way as any other goal round.
func (c *GoalController) AgentEnd(event *agent.AgentEvent) {
	aborted := goalAgentAborted(event)
	c.mu.Lock()
	admission := c.machine.Dispatch(goal.AgentEndEvent{Usage: goalUsageFromAgentEvent(event), Aborted: aborted})
	c.mu.Unlock()
	c.Apply(admission.Effects)
	pending := c.sink.GoalHasPendingMessages()
	c.mu.Lock()
	settle := c.machine.Dispatch(goal.AgentBeforeSettleEvent{Outcome: goalOutcome(event, aborted), HasPendingMessages: pending})
	c.mu.Unlock()
	c.Apply(settle.Effects)
}

// goalUsageFromAgentEvent sums the run's assistant usage.
func goalUsageFromAgentEvent(event *agent.AgentEvent) *goal.GoalUsage {
	if event == nil {
		return nil
	}
	usage := goal.GoalUsage{}
	for _, message := range event.Messages {
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

// goalAgentAborted reports whether the run that ended was cancelled.
func goalAgentAborted(event *agent.AgentEvent) bool {
	if event == nil {
		return false
	}
	for index := len(event.Messages) - 1; index >= 0; index-- {
		if assistant, ok := event.Messages[index].(*ai.AssistantMessage); ok {
			return assistant.StopReason == ai.StopAborted
		}
	}
	return false
}

// goalOutcome classifies the settle: aborted, error, or completed.
func goalOutcome(event *agent.AgentEvent, aborted bool) string {
	if aborted {
		return "aborted"
	}
	if event != nil {
		for index := len(event.Messages) - 1; index >= 0; index-- {
			if assistant, ok := event.Messages[index].(*ai.AssistantMessage); ok {
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
	snapshot := c.Snapshot()
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
			c.notice(*result.Reply, level)
			return
		}
		if !result.IsError && fallback != nil {
			if text := fallback(); text != "" {
				c.notice(text, "info")
			}
		}
	}

	switch command := goal.ParseGoalCommand(args); command.Kind {
	case goal.CmdShowStatus:
		c.notice(goal.GoalStatusMessage(snapshot.Goal, time.Now()), "info")
	case goal.CmdClear:
		if snapshot.Goal == nil {
			c.notice("No goal is set.", "info")
			return
		}
		run(goal.GoalClearEvent{ID: snapshot.Goal.ID, Revision: snapshot.Goal.Revision}, func() string { return "Goal cleared." })
	case goal.CmdPause:
		run(goal.GoalPauseEvent{}, func() string { return "Goal paused." })
	case goal.CmdResume:
		run(goal.GoalResumeEvent{}, func() string { return "Goal resumed." })
	case goal.CmdSet:
		// /goal set overwrites a live goal directly (D233): the old goal is
		// tombstoned so replay still validates every step, the new one is created,
		// and its first round is queued. No confirmation prompt.
		replacing := snapshot.Goal != nil && snapshot.Goal.Phase != goal.PhaseComplete
		run(goal.GoalReplaceEvent{Objective: command.Objective}, func() string {
			if replacing {
				return "Goal replaced."
			}
			return "Goal set."
		})
	case goal.CmdError:
		c.notice(command.Message, "warning")
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
		"Clear only when the goal is no longer relevant or has been superseded; clearing stops goal work, so prefer complete or blocked when they apply.",
	},
}

// Tools returns the three goal tools.
func (c *GoalController) Tools() []agent.AgentTool {
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
		Description: "Complete, block, or clear the session goal. Needs the exact id and revision from get_goal. complete: objective achieved with evidence. blocked: needs blocked_reason; rejected before 3 consecutive rounds. clear: abandon the goal (it is no longer relevant or was superseded); goal work stops and a new goal can be created. pause/resume: human-only (/goal).",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"goal_id":{"type":"string","description":"Exact id from get_goal."},"revision":{"type":"number","description":"Exact revision from get_goal."},"action":{"type":"string","enum":["complete","blocked","clear"],"description":"Action to perform."},"blocked_reason":{"type":"string","description":"Blocking condition (blocked only)."}},"required":["goal_id","revision","action"],"additionalProperties":false}`),
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
			return c.updateGoalToolResult(result, input.Action)
		},
	}
}

// updateGoalToolResult carries the action so the result card can distinguish a
// clear (D232) from a completion.
func (c *GoalController) updateGoalToolResult(result goal.DispatchResult, action string) (agent.AgentToolResult, error) {
	reply := ""
	if result.Reply != nil {
		reply = *result.Reply
	}
	details, _ := json.Marshal(map[string]any{"goal": c.Snapshot().Goal, "action": action})
	return agent.AgentToolResult{
		Content: []ai.Content{ai.TextContent{Text: reply}},
		Details: details,
		IsError: result.IsError,
	}, nil
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
