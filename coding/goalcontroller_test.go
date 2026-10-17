package coding

// Mirrors ~/.pi/agent/extensions/goal/index.test.ts: the host-glue smoke test
// for the controller (effect application, session_start replay, tool closures,
// tool exposure, and the continuation/error/abort settle outcomes).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
)

type recordedGoalEntry struct {
	entryType string
	data      map[string]any
}

type recordedGoalMessage struct {
	customType  string
	content     string
	display     bool
	triggerTurn bool
}

type fakeGoalSink struct {
	entries       []recordedGoalEntry
	messages      []recordedGoalMessage
	notifies      []string
	active        []string
	confirms      int
	confirmResult bool
	pending       bool
	streaming     bool
	runs          int
	branchEntries []goal.CustomEntry
}

func (f *fakeGoalSink) AppendGoalEntry(entryType string, data json.RawMessage) {
	var decoded map[string]any
	_ = json.Unmarshal(data, &decoded)
	f.entries = append(f.entries, recordedGoalEntry{entryType: entryType, data: decoded})
}

func (f *fakeGoalSink) SendGoalMessage(customType string, content string, display bool, details json.RawMessage, triggerTurn bool) {
	f.messages = append(f.messages, recordedGoalMessage{customType: customType, content: content, display: display, triggerTurn: triggerTurn})
}

func (f *fakeGoalSink) RunGoalTurn(ctx context.Context, customType string, content string, display bool, details json.RawMessage) error {
	f.runs++
	f.messages = append(f.messages, recordedGoalMessage{customType: customType, content: content, display: display, triggerTurn: true})
	return nil
}

func (f *fakeGoalSink) GoalIsStreaming() bool { return f.streaming }

func (f *fakeGoalSink) GoalActiveTools() []string { return append([]string{}, f.active...) }

func (f *fakeGoalSink) SetGoalActiveTools(names []string) { f.active = append([]string{}, names...) }

func (f *fakeGoalSink) GoalHasPendingMessages() bool { return f.pending }

func (f *fakeGoalSink) GoalBranchEntries() []goal.CustomEntry { return f.branchEntries }

func goalControllerForSink(sink *fakeGoalSink) *GoalController {
	controller := newGoalController(nil, sink)
	controller.SetUIHooks(
		func(message string, level string) { sink.notifies = append(sink.notifies, message) },
		func(title string, message string, onAnswer func(confirmed bool)) {
			sink.confirms++
			onAnswer(sink.confirmResult)
		},
	)
	return controller
}

func newGoalControllerTest(t *testing.T) (*GoalController, *fakeGoalSink) {
	t.Helper()
	sink := &fakeGoalSink{}
	return goalControllerForSink(sink), sink
}

func findGoalTool(t *testing.T, controller *GoalController, name string) agent.AgentTool {
	t.Helper()
	for _, tool := range controller.Tools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return agent.AgentTool{}
}

func goalToolText(t *testing.T, controller *GoalController, name string, params string) string {
	t.Helper()
	tool := findGoalTool(t, controller, name)
	result, err := tool.Execute("id", json.RawMessage(params), context.Background(), func(agent.AgentToolResult) {})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if text, ok := result.Content[0].(ai.TextContent); ok {
		return text.Text
	}
	return ""
}

func goalAgentEndEvent(assistant *ai.AssistantMessage) *agent.AgentEvent {
	return &agent.AgentEvent{Type: agent.AgentEnd, Messages: []ai.Message{assistant}}
}

func completedAssistant() *ai.AssistantMessage {
	return &ai.AssistantMessage{StopReason: ai.StopStop}
}

func TestGoalControllerRegistersThreeTools(t *testing.T) {
	controller, _ := newGoalControllerTest(t)
	names := []string{}
	for _, tool := range controller.Tools() {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "get_goal,create_goal,update_goal" {
		t.Fatalf("tools = %v", names)
	}
}

func TestGoalControllerSessionStartReplaysAndExposesGoal(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	version := 1
	entry := goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal:      &goal.GoalSnapshot{Version: &version, ID: "g1", Revision: 1, Objective: "obj", Phase: goal.PhaseActive, CreatedAt: 1, UpdatedAt: 1},
		Timestamp: 1,
	}
	data, _ := json.Marshal(entry)
	sink.branchEntries = []goal.CustomEntry{{CustomType: goal.GoalCustomType, Data: data}}
	controller.Start("startup")

	parsed := struct {
		Goal struct {
			Objective string `json:"objective"`
		} `json:"goal"`
		Activation string `json:"activation"`
	}{}
	text := goalToolText(t, controller, "get_goal", "{}")
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Goal.Objective != "obj" || parsed.Activation != "disarmed" {
		t.Fatalf("parsed = %+v", parsed)
	}
	if len(sink.entries) != 0 || len(sink.messages) != 0 {
		t.Fatal("session_start replay wrote")
	}
}

func TestGoalControllerCreateGoalEffect(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	text := goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	if text == "" {
		t.Fatal("create reply empty")
	}
	if len(sink.entries) != 1 || sink.entries[0].data["operation"] != "create" {
		t.Fatalf("entries = %+v", sink.entries)
	}
	if len(sink.messages) != 0 {
		t.Fatal("create queued a message")
	}
}

func TestGoalControllerCreateGoalRejectsEmptyObjective(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	tool := findGoalTool(t, controller, "create_goal")
	result, err := tool.Execute("id", json.RawMessage(`{"objective":"  "}`), context.Background(), func(agent.AgentToolResult) {})
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(sink.entries) != 0 {
		t.Fatal("empty objective wrote an entry")
	}
}

func TestGoalControllerLegacyBudgetArgumentsAreIgnored(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "create_goal", `{"objective":"do it","time_limit_hours":8,"token_limit":500000,"cost_limit_usd":12.5}`)
	if len(sink.entries) != 1 {
		t.Fatalf("entries = %+v", sink.entries)
	}
	if _, ok := sink.entries[0].data["goal"].(map[string]any)["budget"]; ok {
		t.Fatalf("budget leaked: %+v", sink.entries[0].data)
	}
}

func TestGoalControllerAgentEndWithNoGoalProducesNoEffects(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	controller.AgentEnd(goalAgentEndEvent(completedAssistant()))
	if len(sink.entries) != 0 || len(sink.messages) != 0 {
		t.Fatal("agent_end with no goal wrote")
	}
}

func TestGoalControllerAgentEndAdmitsTurnAndContinues(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	sink.entries = nil
	assistant := completedAssistant()
	assistant.Usage = ai.Usage{TotalTokens: 321, Cost: ai.UsageCost{Total: 0.42}}
	controller.AgentEnd(goalAgentEndEvent(assistant))
	if len(sink.entries) != 1 || sink.entries[0].entryType != goal.GoalTurnType {
		t.Fatalf("entries = %+v", sink.entries)
	}
	usage, _ := sink.entries[0].data["usage"].(map[string]any)
	if usage["tokens"] != float64(321) || usage["costUsd"] != 0.42 {
		t.Fatalf("usage = %+v", usage)
	}
	if len(sink.messages) != 1 || !sink.messages[0].triggerTurn || !strings.Contains(sink.messages[0].content, "<goal_round>") {
		t.Fatalf("messages = %+v", sink.messages)
	}
}

// A round queued while the session is idle starts a run (upstream
// sendCustomMessage's not-streaming branch); it is not just queued behind a run
// that will never come.
func TestGoalControllerIdleRoundStartsARun(t *testing.T) {
	sink := &fakeGoalSink{}
	controller := goalControllerForSink(sink)
	started := 0
	controller.SetStartWork(func(fn func(context.Context) error) {
		started++
		if err := fn(context.Background()); err != nil {
			t.Fatal(err)
		}
	})

	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	controller.HandleCommand("pause")
	sink.messages = nil
	sink.runs = 0
	controller.HandleCommand("resume")

	if started != 1 || sink.runs != 1 {
		t.Fatalf("started=%d runs=%d", started, sink.runs)
	}
	if len(sink.messages) != 1 || !sink.messages[0].triggerTurn || !strings.Contains(sink.messages[0].content, "<goal_round>") {
		t.Fatalf("messages = %+v", sink.messages)
	}
}

// A round queued while a run is active follows up instead of starting a nested
// run, and the agent_before_settle transition shares the same delivery path.
func TestGoalControllerRoundWhileStreamingFollowsUp(t *testing.T) {
	sink := &fakeGoalSink{streaming: true}
	controller := goalControllerForSink(sink)
	started := 0
	controller.SetStartWork(func(fn func(context.Context) error) { started++ })

	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	sink.messages = nil
	sink.entries = nil
	controller.AgentEnd(goalAgentEndEvent(completedAssistant()))

	if started != 0 || sink.runs != 0 {
		t.Fatalf("streaming round started a run: started=%d runs=%d", started, sink.runs)
	}
	if len(sink.messages) != 1 || !sink.messages[0].triggerTurn {
		t.Fatalf("messages = %+v", sink.messages)
	}
}

func TestGoalControllerAgentEndErrorPausesWithoutContinuation(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	sink.entries = nil
	assistant := completedAssistant()
	assistant.StopReason = ai.StopError
	controller.AgentEnd(goalAgentEndEvent(assistant))
	if len(sink.messages) != 0 {
		t.Fatalf("error queued a round: %+v", sink.messages)
	}
	if !goalPausedWithReason(sink.entries, "run-error") {
		t.Fatalf("error did not pause: %+v", sink.entries)
	}
}

func TestGoalControllerAgentEndAbortPauses(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	sink.entries = nil
	assistant := completedAssistant()
	assistant.StopReason = ai.StopAborted
	controller.AgentEnd(goalAgentEndEvent(assistant))
	if len(sink.messages) != 0 {
		t.Fatalf("abort queued a round: %+v", sink.messages)
	}
	if !goalPausedWithReason(sink.entries, "cancelled") {
		t.Fatalf("abort did not pause: %+v", sink.entries)
	}
}

func TestGoalControllerPendingMessagesSuppressContinuation(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	sink.entries = nil
	sink.pending = true
	controller.AgentEnd(goalAgentEndEvent(completedAssistant()))
	if len(sink.messages) != 0 {
		t.Fatalf("pending input queued a round: %+v", sink.messages)
	}
}

func goalPausedWithReason(entries []recordedGoalEntry, code string) bool {
	for _, entry := range entries {
		if entry.data["operation"] != "pause" {
			continue
		}
		snapshot, _ := entry.data["goal"].(map[string]any)
		reason, _ := snapshot["blockedReason"].(map[string]any)
		if reason["code"] == code {
			return true
		}
	}
	return false
}

func TestGoalControllerToolExposure(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	controller.Start("startup")
	if strings.Join(sink.active, ",") != "create_goal" {
		t.Fatalf("active = %v", sink.active)
	}
	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	if strings.Join(sink.active, ",") != "create_goal,get_goal,update_goal" {
		t.Fatalf("active = %v", sink.active)
	}
	parsed := struct {
		Goal struct {
			ID       string `json:"id"`
			Revision int    `json:"revision"`
		} `json:"goal"`
	}{}
	_ = json.Unmarshal([]byte(goalToolText(t, controller, "get_goal", "{}")), &parsed)
	revision, _ := json.Marshal(parsed.Goal.Revision)
	goalToolText(t, controller, "update_goal", `{"goal_id":"`+parsed.Goal.ID+`","revision":`+string(revision)+`,"action":"complete"}`)
	if strings.Join(sink.active, ",") != "create_goal" {
		t.Fatalf("active = %v", sink.active)
	}
}

func TestGoalControllerCommandReplaceConfirm(t *testing.T) {
	sink := &fakeGoalSink{confirmResult: true}
	controller := goalControllerForSink(sink)
	goalToolText(t, controller, "create_goal", `{"objective":"first"}`)
	sink.entries = nil
	controller.HandleCommand("set second")
	if sink.confirms != 1 {
		t.Fatalf("confirms = %d", sink.confirms)
	}
	ops := []string{}
	for _, entry := range sink.entries {
		if op, ok := entry.data["operation"].(string); ok {
			ops = append(ops, op)
		}
	}
	if strings.Join(ops, ",") != "clear,create" {
		t.Fatalf("ops = %v", ops)
	}

	// Declining leaves the goal untouched.
	sink = &fakeGoalSink{confirmResult: false}
	controller = goalControllerForSink(sink)
	goalToolText(t, controller, "create_goal", `{"objective":"first"}`)
	sink.entries = nil
	controller.HandleCommand("set second")
	if len(sink.entries) != 0 {
		t.Fatalf("declined replace wrote %+v", sink.entries)
	}
}

func TestGoalControllerCommandStatus(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "create_goal", `{"objective":"show me"}`)
	sink.notifies = nil
	controller.HandleCommand("status")
	if len(sink.notifies) != 1 || !strings.Contains(sink.notifies[0], "show me") {
		t.Fatalf("notifies = %v", sink.notifies)
	}
}

func TestGoalRoundReminderDoesNotReusePreviousGoalsTurn(t *testing.T) {
	controller, _ := newGoalControllerTest(t)
	controller.mu.Lock()
	result := controller.machine.Dispatch(goal.GoalSetEvent{Objective: "current objective"})
	controller.mu.Unlock()
	if result.IsError {
		t.Fatalf("set goal: %v", result.Reply)
	}
	previous := goalEventMessage(
		goal.GoalEventType,
		"<goal_round><untrusted_objective>previous objective</untrusted_objective> Round 1.</goal_round>",
		false,
		json.RawMessage(`{"kind":"round","turn":1,"goalId":"previous-goal"}`),
	)
	reminder := controller.roundPromptIfMissing([]ai.Message{previous})
	if reminder == nil {
		t.Fatal("previous goal's turn 1 prompt suppressed the current goal's reminder")
	}
	var fields customMessageFields
	if err := json.Unmarshal(reminder.Content, &fields); err != nil {
		t.Fatal(err)
	}
	var content string
	if err := json.Unmarshal(fields.Content, &content); err != nil || !strings.Contains(content, "current objective") {
		t.Fatalf("reconciled prompt = %q err = %v", content, err)
	}
	var details struct {
		GoalID string `json:"goalId"`
	}
	if err := json.Unmarshal(fields.Details, &details); err != nil || details.GoalID != controller.Snapshot().Goal.ID {
		t.Fatalf("reconciled goal id = %q err = %v", details.GoalID, err)
	}
}

// TestGoalEventMessageIsInvisibleButReachesTheModel pins that the goal round is
// a display:false custom message (so it stays out of the transcript) whose
// content still converts to a model-visible user message.
func TestGoalEventMessageIsInvisibleButReachesTheModel(t *testing.T) {
	message := goalEventMessage(goal.GoalEventType, "<goal_round>round 1</goal_round>", false, json.RawMessage(`{"kind":"round","turn":1}`))
	if message.Role != RoleCustom {
		t.Fatalf("role = %q", message.Role)
	}
	var fields customMessageFields
	if err := json.Unmarshal(message.Content, &fields); err != nil {
		t.Fatal(err)
	}
	if fields.CustomType != goal.GoalEventType || fields.Display {
		t.Fatalf("fields = %+v", fields)
	}
	var text string
	if err := json.Unmarshal(fields.Content, &text); err != nil || !strings.Contains(text, "<goal_round>") {
		t.Fatalf("content = %q err = %v", string(fields.Content), err)
	}

	converted := ConvertToLlm([]ai.Message{message})
	if len(converted) != 1 {
		t.Fatalf("converted = %d", converted)
	}
	user, ok := converted[0].(*ai.UserMessage)
	if !ok || len(user.Content.Blocks) == 0 {
		t.Fatalf("converted = %#v", converted[0])
	}
	block, ok := user.Content.Blocks[0].(ai.TextContent)
	if !ok || !strings.Contains(block.Text, "<goal_round>") {
		t.Fatalf("block = %#v", user.Content.Blocks[0])
	}
}

func TestNewAgentSessionReplaysGoalAndExposesTools(t *testing.T) {
	manager, dir := newTestSession(t)
	version := 1
	entry, _ := json.Marshal(goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal:      &goal.GoalSnapshot{Version: &version, ID: "g1", Revision: 1, Objective: "obj", Phase: goal.PhaseActive, CreatedAt: 1, UpdatedAt: 1},
		Timestamp: 1,
	})
	manager.AppendCustomEntry(goal.GoalCustomType, entry)

	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn,
		Goal:    true,
		Control: &AgentSessionControl{},
	})
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Join(session.ActiveToolNames(), ",")
	if names != "create_goal,get_goal,update_goal" {
		t.Fatalf("active tools = %q", names)
	}
	snapshot := session.Goal.Snapshot()
	if snapshot.Goal == nil || snapshot.Goal.Objective != "obj" || snapshot.Armed {
		t.Fatalf("goal = %+v", snapshot.Goal)
	}
	if session.GetToolDefinition("get_goal") == nil {
		t.Fatal("get_goal not registered")
	}
}

// TestGoalContinuationReachesTheModel proves the settle-boundary continuation:
// a creating run admits its round and the hidden goal round is delivered to
// the next model request as a user message.
func TestGoalContinuationReachesTheModel(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}

	var session *AgentSession
	var calls int
	var secondContext []ai.Message
	streamFn := func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		if calls == 2 {
			secondContext = append([]ai.Message{}, transcript.Messages...)
			// Complete the goal through its real tool path so the unbudgeted
			// round loop ends here.
			if snapshot := session.Goal.Snapshot().Goal; snapshot != nil {
				params, _ := json.Marshal(map[string]any{"goal_id": snapshot.ID, "revision": snapshot.Revision, "action": "complete"})
				tool := session.control.Tools["update_goal"].Tool
				_, _ = tool.Execute("complete", params, context.Background(), func(agent.AgentToolResult) {})
			}
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "ack"}}, StopReason: ai.StopStop,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}

	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn,
		Tools:        []agent.AgentTool{testTool("read")},
		ConvertToLlm: ConvertToLlm,
		Goal:         true,
		Control: &AgentSessionControl{
			autoCompaction: true, autoRetry: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	create := session.control.Tools["create_goal"].Tool
	if _, err := create.Execute("id", json.RawMessage(`{"objective":"do it"}`), context.Background(), func(agent.AgentToolResult) {}); err != nil {
		t.Fatal(err)
	}
	if err := session.Prompt(context.Background(), "start", &PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("stream calls = %d, want the continuation round", calls)
	}
	found := false
	for _, message := range secondContext {
		if user, ok := message.(*ai.UserMessage); ok && strings.Contains(contentTextJoinedNoSep(user.Content), "<goal_round>") {
			found = true
		}
	}
	if !found {
		t.Fatalf("round prompt missing from the continuation context: %d messages", len(secondContext))
	}
	if phase := session.Goal.Snapshot().Goal.Phase; phase != goal.PhaseComplete {
		t.Fatalf("goal phase = %s", phase)
	}
}

func TestGoalControllerBuffersNoticesUntilUIHooks(t *testing.T) {
	version := 1
	data, _ := json.Marshal(goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal:      &goal.GoalSnapshot{Version: &version, ID: "g1", Revision: 1, Objective: "obj", Phase: goal.PhaseActive, CreatedAt: 1, UpdatedAt: 1},
		Timestamp: 1,
	})
	sink := &fakeGoalSink{branchEntries: []goal.CustomEntry{{CustomType: goal.GoalCustomType, Data: data}}}
	controller := newGoalController(nil, sink)

	controller.Start("startup")
	if len(sink.notifies) != 0 {
		t.Fatalf("notice delivered before hooks: %v", sink.notifies)
	}
	controller.SetUIHooks(func(message string, level string) { sink.notifies = append(sink.notifies, message) }, nil)
	if len(sink.notifies) != 1 || !strings.Contains(sink.notifies[0], "Goal restored") {
		t.Fatalf("buffered notice = %v", sink.notifies)
	}
}
