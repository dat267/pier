package coding

// The goal controller is a session feature: it owns the goal machine and
// applies its effects against the session it belongs to. It is built in
// NewAgentSession like the cache warmer, not passed in as an object graph from
// the SDK.

import (
	"context"
	"encoding/json"
	"fmt"
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
	// willRetry reports whether the run that just ended will auto-retry. A
	// retried failure is not final, so the goal must not pause on it.
	willRetry() bool
}

// sessionGoalSink adapts an AgentSession to goalSink.
type sessionGoalSink struct{ session *AgentSession }

// willRetry reports whether the failed run will auto-retry (nil session: no).
func (s *sessionGoalSink) willRetry() bool { return s.session != nil && s.session.willRetry }

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
	details, _ := ai.MarshalJSON(goalRoundDetails(snapshot.Goal.ID, turn))
	return goalEventMessage(goal.GoalEventType, content, false, details)
}

// Apply executes the machine's effects against the session.
func (c *GoalController) Apply(effects []goal.Effect) {
	mutated := false
	for _, effect := range effects {
		switch typed := effect.(type) {
		case goal.AppendEntryEffect:
			data, err := ai.MarshalJSON(typed.Data)
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
			details, _ := ai.MarshalJSON(detailValues)
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
			details, _ := ai.MarshalJSON(detailValues)
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

// AgentEnd admits goal work and settles its round. A retryable failure stays
// active for the session's retry; final outcomes may pause or queue the next
// round. Both hook effects go through Apply.
func (c *GoalController) AgentEnd(event *agent.AgentEvent) {
	aborted := goalAgentAborted(event)
	willRetry := c.sink.willRetry() // D242: don't pause a failure the session will retry.
	c.mu.Lock()
	admission := c.machine.Dispatch(goal.AgentEndEvent{Usage: goalUsageFromAgentEvent(event), Aborted: aborted})
	c.mu.Unlock()
	c.Apply(admission.Effects)
	pending := c.sink.GoalHasPendingMessages()
	c.mu.Lock()
	settle := c.machine.Dispatch(goal.AgentBeforeSettleEvent{Outcome: goalOutcome(event, aborted, willRetry), HasPendingMessages: pending})
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

// goalOutcome classifies the settle: aborted, retrying, error, or completed. A
// retried failure is not final: the goal stays armed and the round re-runs when
// the retry succeeds.
func goalOutcome(event *agent.AgentEvent, aborted bool, willRetry bool) string {
	if aborted {
		return "aborted"
	}
	if willRetry {
		return "retrying"
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

// goalToolSnippets are the goal tools' one-line prompt entries (upstream
// promptSnippet). They list the tools in the system prompt's tools section.
var goalToolSnippets = map[string]string{
	"get_goal": "Read the current goal objective and state",
	"set_goal": "Set, replace, or end the session goal",
}

// goalToolGuidelines supplies the goal tools' prompt bullets.
var goalToolGuidelines = map[string][]string{
	"get_goal": {"Read get_goal before set_goal when you need the current state."},
	"set_goal": {
		"Use set_goal with an objective when the user's request is a multi-step objective that should continue across rounds.",
		"Do not set goals for trivial single-turn work.",
		"Before setting, turn the request into a concrete objective with outcome, verification, constraints, and boundaries.",
		"Setting an objective replaces a live goal.",
		"Mark complete only when the objective is actually achieved, with evidence.",
		"Mark blocked only after the same condition persisted for at least 3 consecutive goal rounds.",
		"Clear only when the goal is no longer relevant or has been superseded; clearing stops goal work, so prefer complete or blocked when they apply.",
	},
}

// Tools returns the two goal tools: get_goal reads and set_goal writes.
func (c *GoalController) Tools() []agent.AgentTool {
	return []agent.AgentTool{c.getGoalTool(), c.setGoalTool()}
}

func (c *GoalController) getGoalTool() agent.AgentTool {
	return agent.AgentTool{
		Name:        "get_goal",
		Label:       "Get Goal",
		Description: "Read the current session goal.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Execute: func(_ string, _ json.RawMessage, _ context.Context, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			view := c.Snapshot().Goal
			payload, err := ai.MarshalIndentJSON(goal.GoalViewOf(view))
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			details, _ := ai.MarshalJSON(map[string]any{"goal": view})
			return agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: string(payload)}},
				Details: details,
			}, nil
		},
	}
}

// setGoalTool is the one write tool: an objective starts or replaces the goal, a
// status ends it. The exact id/revision guard is gone, so the model never has to
// copy a ref back (D234). Nullable properties also support callers that expose
// every argument as required without applying strict-schema conversion: unused
// objective/status/blocked_reason values can be null instead of conflicting.
func (c *GoalController) setGoalTool() agent.AgentTool {
	return agent.AgentTool{
		Name:        "set_goal",
		Label:       "Set Goal",
		Description: "Set, replace, or end the session goal. objective: start the goal, or replace a live one. status: end it - complete (objective achieved, with evidence), blocked (blocked_reason required; only after 3 consecutive goal rounds), or clear (no longer relevant or superseded). Provide exactly one non-null objective or status. Omit unused fields or set them to null.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"objective":{"anyOf":[{"type":"string"},{"type":"null"}],"description":"Concrete completion objective. Starts or replaces the goal. Omit or set to null when ending a goal."},"status":{"anyOf":[{"type":"string","enum":["complete","blocked","clear"]},{"type":"null"}],"description":"Ends the current goal. Omit or set to null when setting an objective."},"blocked_reason":{"anyOf":[{"type":"string"},{"type":"null"}],"description":"Blocking condition (blocked only). Otherwise omit or set to null."}},"additionalProperties":false}`),
		Execute: func(_ string, params json.RawMessage, _ context.Context, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			var input struct {
				Objective     string `json:"objective"`
				Status        string `json:"status"`
				BlockedReason string `json:"blocked_reason"`
			}
			if err := json.Unmarshal(params, &input); err != nil {
				return goalErrorResult(err.Error())
			}
			objective := strings.TrimSpace(input.Objective)
			status := strings.TrimSpace(input.Status)
			switch {
			case objective != "" && status != "":
				return goalErrorResult("Provide either objective to set the goal or status to end it, not both.")
			case objective == "" && status == "":
				return goalErrorResult("Provide an objective to set the goal or a status to end it.")
			case objective != "":
				c.mu.Lock()
				result := c.machine.Dispatch(goal.GoalAgentSetEvent{Objective: objective})
				c.mu.Unlock()
				c.Apply(result.Effects)
				return c.toolResult(result, "Goal set.")
			}

			switch status {
			case "complete", "blocked", "clear":
			default:
				return goalErrorResult(fmt.Sprintf(`Unknown status %q. Use "complete", "blocked", or "clear".`, status))
			}
			current := c.Snapshot().Goal
			if current == nil {
				return goalErrorResult("No goal is set.")
			}
			c.mu.Lock()
			var (
				result   goal.DispatchResult
				fallback string
			)
			switch status {
			case "complete":
				result = c.machine.Dispatch(goal.GoalUpdateEvent{GoalID: current.ID, Revision: current.Revision, Action: "complete"})
				fallback = "Goal complete."
			case "blocked":
				result = c.machine.Dispatch(goal.GoalUpdateEvent{GoalID: current.ID, Revision: current.Revision, Action: "blocked", BlockedReason: input.BlockedReason})
				fallback = "Goal blocked."
			case "clear":
				result = c.machine.Dispatch(goal.GoalClearEvent{ID: current.ID, Revision: current.Revision})
				fallback = "Goal cleared."
			}
			c.mu.Unlock()
			c.Apply(result.Effects)
			return c.setGoalToolResult(result, status, fallback)
		},
	}
}

// setGoalToolResult carries the status so the result card can distinguish a
// clear from a completion.
func (c *GoalController) setGoalToolResult(result goal.DispatchResult, status string, fallback string) (agent.AgentToolResult, error) {
	reply := fallback
	if result.Reply != nil {
		reply = *result.Reply
	}
	details, _ := ai.MarshalJSON(map[string]any{"goal": c.Snapshot().Goal, "action": status})
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
	details, _ := ai.MarshalJSON(map[string]any{"goal": c.Snapshot().Goal})
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
	contentJSON, err := ai.MarshalJSON(content)
	if err != nil {
		contentJSON = json.RawMessage(`""`)
	}
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	fields, err := ai.MarshalJSON(customMessageFields{
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
