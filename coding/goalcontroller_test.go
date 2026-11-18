package coding

import (
	"context"
	"encoding/json"
	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
	"math"
	"strings"
	"testing"
	"time"
)

// Mirrors ~/.pi/agent/extensions/goal/index.test.ts: the host-glue smoke test
// for the controller (effect application, session_start replay, tool closures,
// tool exposure, and the continuation/error/abort settle outcomes).

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
	rawEntries    [][]byte
	messages      []recordedGoalMessage
	notifies      []string
	active        []string
	pending       bool
	streaming     bool
	runs          int
	branchEntries []goal.CustomEntry
	retry         bool
	emitted       []*SessionEntry
}

func (f *fakeGoalSink) AppendGoalEntry(entryType string, data json.RawMessage) *SessionEntry {
	var decoded map[string]any
	_ = json.Unmarshal(data, &decoded)
	f.entries = append(f.entries, recordedGoalEntry{entryType: entryType, data: decoded})
	f.rawEntries = append(f.rawEntries, append([]byte(nil), data...))
	return &SessionEntry{Type: "custom", CustomType: entryType, Data: append(json.RawMessage(nil), data...)}
}

func (f *fakeGoalSink) EmitGoalEntryAppended(entry *SessionEntry) {
	f.emitted = append(f.emitted, entry)
}

func (f *fakeGoalSink) SendGoalMessage(customType string, content string, display bool, details json.RawMessage, triggerTurn bool) bool {
	f.messages = append(f.messages, recordedGoalMessage{customType: customType, content: content, display: display, triggerTurn: triggerTurn})
	return true
}

func (f *fakeGoalSink) RunGoalTurn(ctx context.Context, customType string, content string, display bool, details json.RawMessage) error {
	f.runs++
	f.messages = append(f.messages, recordedGoalMessage{customType: customType, content: content, display: display, triggerTurn: true})
	return nil
}

func (f *fakeGoalSink) GoalIsStreaming() bool { return f.streaming }

func (f *fakeGoalSink) SyncGoalTools() {
	if next := goal.DesiredToolNames(f.active); !sameToolSet(next, f.active) {
		f.active = append([]string{}, next...)
	}
}

func (f *fakeGoalSink) GoalHasPendingMessages() bool { return f.pending }

func (f *fakeGoalSink) GoalBranchEntries() []goal.CustomEntry { return f.branchEntries }

func (f *fakeGoalSink) willRetry() bool { return f.retry }

func goalControllerForSink(sink *fakeGoalSink) *GoalController {
	controller := newGoalController(nil, sink)
	controller.SetUIHooks(func(message string, level string) { sink.notifies = append(sink.notifies, message) })
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

// Goal durable entries and custom messages are persisted session payloads, so
// they marshal through ai.MarshalJSON (no HTML escaping of <, >, &). Goals are
// port-local, so byte parity is against the extension's JSON.stringify, which
// does not escape those either.
func TestGoalPayloadsDoNotHTMLEscape(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"fix <b> & <i>"}`)
	if len(sink.rawEntries) == 0 {
		t.Fatal("no durable entry recorded")
	}
	entry := string(sink.rawEntries[len(sink.rawEntries)-1])
	if strings.Contains(entry, `\u003c`) || strings.Contains(entry, `\u0026`) {
		t.Fatalf("goal entry HTML-escaped: %s", entry)
	}
	if !strings.Contains(entry, "fix <b> & <i>") {
		t.Fatalf("goal entry lost the objective text: %s", entry)
	}

	content := goal.PauseContext("ship <it> & co", goal.BlockedReason{Code: "human-paused", Message: "Paused by user."})
	message := goalEventMessage(goal.GoalEventType, content, false, json.RawMessage(`{"kind":"paused"}`))
	if strings.Contains(string(message.Content), `\u003c`) || strings.Contains(string(message.Content), `\u0026`) {
		t.Fatalf("goal message HTML-escaped: %s", message.Content)
	}
	if !strings.Contains(string(message.Content), "ship <it> & co") {
		t.Fatalf("goal message lost the content: %s", message.Content)
	}
}

func TestGoalControllerRegistersTwoTools(t *testing.T) {
	controller, _ := newGoalControllerTest(t)
	names := []string{}
	for _, tool := range controller.Tools() {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "get_goal,set_goal" {
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
	text := goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
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
	tool := findGoalTool(t, controller, "set_goal")
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
	goalToolText(t, controller, "set_goal", `{"objective":"do it","time_limit_hours":8,"token_limit":500000,"cost_limit_usd":12.5}`)
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
	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
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

	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
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

	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
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
	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
	sink.entries = nil
	assistant := completedAssistant()
	assistant.StopReason = ai.StopError
	controller.AgentEnd(goalAgentEndEvent(assistant))
	// D239: a pause reports the stop to the model, but never queues a round.
	if len(sink.messages) != 1 || sink.messages[0].triggerTurn || !strings.Contains(sink.messages[0].content, "<goal_paused>") {
		t.Fatalf("error queued a round: %+v", sink.messages)
	}
	if !goalPausedWithReason(sink.entries, "run-error") {
		t.Fatalf("error did not pause: %+v", sink.entries)
	}
}

func TestGoalControllerAgentEndAbortPauses(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
	sink.entries = nil
	assistant := completedAssistant()
	assistant.StopReason = ai.StopAborted
	controller.AgentEnd(goalAgentEndEvent(assistant))
	// D239: a pause reports the stop to the model, but never queues a round.
	if len(sink.messages) != 1 || sink.messages[0].triggerTurn || !strings.Contains(sink.messages[0].content, "<goal_paused>") {
		t.Fatalf("abort queued a round: %+v", sink.messages)
	}
	if !goalPausedWithReason(sink.entries, "cancelled") {
		t.Fatalf("abort did not pause: %+v", sink.entries)
	}
}

func TestGoalControllerPendingMessagesSuppressContinuation(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
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

// D418: both goal tools are active for the whole session, so no transition
// changes the tool array or the prompt sections a prefix cache covers.
func TestGoalControllerToolExposure(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	controller.Start("startup")
	if strings.Join(sink.active, ",") != "set_goal,get_goal" {
		t.Fatalf("active = %v", sink.active)
	}
	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
	if strings.Join(sink.active, ",") != "set_goal,get_goal" {
		t.Fatalf("creating a goal changed the active set: %v", sink.active)
	}
	goalToolText(t, controller, "set_goal", `{"status":"complete"}`)
	if strings.Join(sink.active, ",") != "set_goal,get_goal" {
		t.Fatalf("completing a goal changed the active set: %v", sink.active)
	}
}

// /goal set over a live goal overwrites it directly (D233): the old goal is
// tombstoned, the new one is created and armed, and no confirmation is asked.
func TestGoalControllerSetOverwritesLiveGoal(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"first"}`)
	sink.entries = nil
	controller.HandleCommand("set second")

	ops := []string{}
	for _, entry := range sink.entries {
		if op, ok := entry.data["operation"].(string); ok {
			ops = append(ops, op)
		}
	}
	if strings.Join(ops, ",") != "clear,create" {
		t.Fatalf("ops = %v", ops)
	}
	snapshot := controller.Snapshot()
	if snapshot.Goal == nil || snapshot.Goal.Objective != "second" {
		t.Fatalf("goal = %+v", snapshot.Goal)
	}
	if !snapshot.Armed {
		t.Fatal("replaced goal must be armed")
	}
}

// A fresh /goal set creates and arms the goal and queues its first round.
func TestGoalControllerSetFreshGoal(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	controller.HandleCommand("set first")
	snapshot := controller.Snapshot()
	if snapshot.Goal == nil || snapshot.Goal.Objective != "first" || !snapshot.Armed {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(sink.messages) != 1 || !sink.messages[0].triggerTurn {
		t.Fatalf("round = %+v", sink.messages)
	}
	if len(sink.entries) != 1 || sink.entries[0].data["operation"] != "create" {
		t.Fatalf("entries = %+v", sink.entries)
	}
}

func TestGoalControllerCommandStatus(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"show me"}`)
	sink.notifies = nil
	controller.HandleCommand("status")
	if len(sink.notifies) != 1 || !strings.Contains(sink.notifies[0], "show me") {
		t.Fatalf("notifies = %v", sink.notifies)
	}
}

// The agent may clear its own goal through set_goal (port-local, D232); the
// tool schema advertises the status and clearing removes the goal.
func TestGoalControllerSetGoalClear(t *testing.T) {
	controller, _ := newGoalControllerTest(t)
	if params := string(findGoalTool(t, controller, "set_goal").Parameters); !strings.Contains(params, `"clear"`) {
		t.Fatalf("set_goal schema has no clear status: %s", params)
	}
	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
	reply := goalToolText(t, controller, "set_goal", `{"status":"clear"}`)
	if controller.Snapshot().Goal != nil {
		t.Fatalf("goal still present: %+v", controller.Snapshot().Goal)
	}
	if !strings.Contains(reply, "cleared") {
		t.Fatalf("reply = %q", reply)
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
	if names != "set_goal,get_goal" {
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
				_ = snapshot
				params := json.RawMessage(`{"status":"complete"}`)
				tool := session.control.Tools["set_goal"].Tool
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
	create := session.control.Tools["set_goal"].Tool
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
	controller.SetUIHooks(func(message string, level string) { sink.notifies = append(sink.notifies, message) })
	if len(sink.notifies) != 1 || !strings.Contains(sink.notifies[0], "Goal restored") {
		t.Fatalf("buffered notice = %v", sink.notifies)
	}
}

// D223 ports goal/machine.ts's resume lifecycle. The local phase graph in
// internal/goal/state.go makes complete terminal, so rejecting /goal resume
// must leave both the durable goal and its process-local activation unchanged.
// The extension is not part of pinned pi 4a080252 and is unavailable here.
func TestGoalResumeLeavesCompletedGoalDisarmed(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"finish it"}`)
	goalToolText(t, controller, "set_goal", `{"status":"complete"}`)
	before := goalToolText(t, controller, "get_goal", `{}`)
	entries, messages := len(sink.entries), len(sink.messages)

	controller.HandleCommand("resume")

	var view goal.GoalViewResult
	after := goalToolText(t, controller, "get_goal", `{}`)
	if err := json.Unmarshal([]byte(after), &view); err != nil {
		t.Fatal(err)
	}
	if view.Goal == nil || view.Goal.Phase != goal.PhaseComplete || view.Activation != "disarmed" {
		t.Fatalf("resume changed terminal goal: %s", after)
	}
	if after != before {
		t.Fatalf("rejected resume changed goal\nbefore: %s\nafter: %s", before, after)
	}
	if len(sink.entries) != entries || len(sink.messages) != messages || sink.runs != 0 {
		t.Fatalf("rejected resume delivered work: entries=%d messages=%d runs=%d", len(sink.entries)-entries, len(sink.messages)-messages, sink.runs)
	}
}

// panicOnceGoalSink models a session listener that throws on the first published
// goal entry. Agent loop tool execution recovers a thrown/panicking tool call
// into an error result (agent/loop.go), and upstream's synchronous dispatcher
// remains usable after a listener throws.
type panicOnceGoalSink struct {
	*fakeGoalSink
	panicNext bool
}

func (s *panicOnceGoalSink) EmitGoalEntryAppended(entry *SessionEntry) {
	if s.panicNext {
		s.panicNext = false
		panic("goal listener exploded")
	}
	s.fakeGoalSink.EmitGoalEntryAppended(entry)
}

func TestGoalPublisherResumesAfterRecoveredListenerPanic(t *testing.T) {
	sink := &panicOnceGoalSink{fakeGoalSink: &fakeGoalSink{}, panicNext: true}
	controller := newGoalController(nil, sink)
	tool := findGoalTool(t, controller, "set_goal")
	call := func(params string) (recovered any) {
		defer func() { recovered = recover() }()
		_, _ = tool.Execute("id", json.RawMessage(params), context.Background(), func(agent.AgentToolResult) {})
		return nil
	}

	if recovered := call(`{"objective":"first"}`); recovered == nil {
		t.Fatal("the listener panic did not reach the tool dispatcher")
	}
	if len(sink.entries) != 1 {
		t.Fatalf("durable entries = %d, want the first create", len(sink.entries))
	}
	if recovered := call(`{"objective":"second"}`); recovered != nil {
		t.Fatalf("second goal mutation panicked: %v", recovered)
	}
	if len(sink.emitted) != 2 {
		t.Fatalf("published entries after recovery = %d, want the replacement clear/create pair", len(sink.emitted))
	}
}

// Upstream sendCustomMessage builds {role, customType, content, display,
// details, timestamp: Date.now()} (agent-session.ts v1.5.2 b4afdddf). A goal
// message enters the agent transcript in that shape, stamped, so the agent's
// copy matches the message a reload builds from its session entry.
func TestGoalEventMessageIsStampedInTheUpstreamCustomShape(t *testing.T) {
	message := goalEventMessage("goal-event", "keep going", true, json.RawMessage(`{"n":1}`))
	if message.Timestamp == 0 {
		t.Fatal("the goal message carries no timestamp")
	}
	got, err := ai.MarshalJSON(message)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ai.MarshalJSON(CreateCustomMessage("goal-event", json.RawMessage(`"keep going"`), true, json.RawMessage(`{"n":1}`), message.Timestamp))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("goal message\n got %s\nwant %s", got, want)
	}
}

// A settle that leaves a released follow-up run behind says so, so the working
// period stays open across the release instead of closing and restarting
// (D402). The released run's own settle reports nothing pending.
func TestGoalSettleReportsThePendingFollowUpRun(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	var session *AgentSession
	var settled []bool
	round := 0
	streamFn := func(m *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		round++
		if round == 2 {
			_, _ = session.GetToolDefinition("set_goal").Execute("complete", json.RawMessage(`{"status":"complete"}`), context.Background(), nil)
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "done"}},
			}})
		}()
		return stream
	}
	var err error
	session, err = NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, Goal: true, ConvertToLlm: ConvertToLlm,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.Subscribe(func(event *SessionEvent) {
		if event.Type == SessionAgentSettled {
			settled = append(settled, event.FollowUpsPending)
		}
	})
	var pending []func(context.Context) error
	session.Goal.SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
	session.FollowUp(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "after the goal"}})
	session.Goal.HandleCommand("set objective")
	for index := 0; index < len(pending); index++ {
		if err := pending[index](context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(settled) != 2 {
		t.Fatalf("settled events = %v, want the goal chain and the released run", settled)
	}
	if !settled[0] {
		t.Error("the goal chain's settle did not report the released follow-up run")
	}
	if settled[1] {
		t.Error("the released run's settle still reported pending follow-ups")
	}
}

// D223: active goals expose get_goal before their model round starts. The
// extension's nonblocking sendMessage leaves time for syncGoalTools before
// provider dispatch; SDK idle delivery here can start a model run inline.
func TestIdleGoalRoundReceivesActiveGoalTools(t *testing.T) {
	for _, commandStartsRound := range []bool{true, false} {
		name := "idle-command"
		if !commandStartsRound {
			name = "tool-create-control"
		}
		t.Run(name, func(t *testing.T) {
			manager, dir := newTestSession(t)
			model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
			var session *AgentSession
			calls := 0
			sawGetGoal := false
			streamFn := func(model *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				calls++
				for _, tool := range ai.GetCurrentTools(transcript.Messages) {
					if tool.Name == "get_goal" {
						sawGetGoal = true
					}
				}
				_, _ = session.GetToolDefinition("set_goal").Execute("complete", json.RawMessage(`{"status":"complete"}`), context.Background(), nil)
				stream := ai.NewAssistantMessageEventStream()
				go func() {
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
						API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
						Content: ai.ContentList{ai.TextContent{Text: "done"}},
					}})
				}()
				return stream
			}
			var err error
			session, err = NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, Goal: true, ConvertToLlm: ConvertToLlm})
			if err != nil {
				t.Fatal(err)
			}
			if commandStartsRound {
				session.Goal.HandleCommand("set finish the objective")
			} else {
				if _, err := session.GetToolDefinition("set_goal").Execute("create", json.RawMessage(`{"objective":"finish the objective"}`), context.Background(), nil); err != nil {
					t.Fatal(err)
				}
				if err := session.Prompt(context.Background(), "continue", nil); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatalf("model calls = %d, want 1", calls)
			}
			if !sawGetGoal {
				t.Fatal("active goal's first model request did not expose get_goal")
			}
		})
	}
}

// D223's installed goal extension, index.ts set_goal.execute, trims objectives
// with JavaScript String.trim. Source read SHA-256:
// 5b0d421772a0b3c37f6486092acf9301aeaf7bc2aa288574dff47ba1ce3d662e.
// Stock pi v1.4.0 has no goal tool. D234 changes tool actions, not objective
// whitespace: BOM-only is empty, and U+0085 is not JavaScript whitespace.
func TestSessionGoalObjectiveUsesJavaScriptTrim(t *testing.T) {
	for _, fixture := range []struct {
		name, objective, want string
		reject                bool
	}{
		{"bom-only", "\uFEFF", "", true},
		{"bom-boundaries", "\uFEFFfix\uFEFF", "fix", false},
		{"next-line-control", "\u0085fix\u0085", "\u0085fix\u0085", false},
		{"ascii-control", " fix ", "fix", false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			manager, dir := newTestSession(t)
			session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, Goal: true})
			if err != nil {
				t.Fatal(err)
			}
			tool := session.GetToolDefinition("set_goal")
			if tool == nil {
				t.Fatal("set_goal not registered")
			}
			params, err := ai.MarshalJSON(map[string]string{"objective": fixture.objective})
			if err != nil {
				t.Fatal(err)
			}
			result, err := tool.Execute("call", json.RawMessage(params), context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != fixture.reject {
				t.Fatalf("IsError = %v, want %v: %+v", result.IsError, fixture.reject, result.Content)
			}
			snapshot := session.Goal.Snapshot().Goal
			if fixture.reject {
				if snapshot != nil {
					t.Fatalf("blank objective created a goal: %+v", snapshot)
				}
			} else if snapshot == nil || snapshot.Objective != fixture.want {
				t.Fatalf("goal = %+v, want objective %q", snapshot, fixture.want)
			}
		})
	}
}

// D223: the goal is durable branch state, replayed from the branch at
// startup. Navigating the session tree moves the branch, so the goal must be
// the one the new branch holds: a goal set after the navigation target is not
// on the new branch, and a command after navigating must not persist a
// transition against it, or the branch no longer replays.
func TestGoalFollowsTheBranchAcrossTreeNavigation(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: stubStreamFn, Goal: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	session.Goal.SetStartWork(func(func(context.Context) error) {})

	if err := session.Prompt(context.Background(), "before the goal", nil); err != nil {
		t.Fatal(err)
	}
	forks := session.GetUserMessagesForForking()
	if len(forks) != 1 {
		t.Fatalf("fork targets = %+v", forks)
	}
	session.Goal.HandleCommand("set ship the release")
	if session.Goal.Snapshot().Goal == nil {
		t.Fatal("the goal was not set")
	}

	// Back to before the goal existed: the branch holds no goal now.
	if _, err := session.NavigateTree(context.Background(), forks[0].EntryID, NavigateTreeOptions{}); err != nil {
		t.Fatal(err)
	}
	if goal := session.Goal.Snapshot().Goal; goal != nil {
		t.Fatalf("after navigating to before the goal, the controller still holds %q", goal.Objective)
	}

	// A command now must persist against the branch's goal state, and the
	// branch must replay without a corruption warning.
	session.Goal.HandleCommand("set a different objective")
	var warnings []string
	session.Goal.SetUIHooks(func(message, level string) {
		if level == "warning" && strings.Contains(message, "corrupt") {
			warnings = append(warnings, message)
		}
	})
	session.Goal.Start("startup")
	if len(warnings) > 0 {
		t.Fatalf("the branch does not replay after navigating: %v", warnings)
	}
	if goal := session.Goal.Snapshot().Goal; goal == nil || goal.Objective != "a different objective" {
		t.Fatalf("replayed goal = %+v", goal)
	}
}

// D223's first-party goal contract accounts every assistant in an agent_end
// before checking the restored budget. Reference: pi v1.5.0 (f9a26365),
// packages/agent/src/agent-loop.ts and coding-agent/src/core/usage-totals.ts
// accumulate messages/usage without signed integer wraparound.
func TestGoalRoundUsageDoesNotOverflowBeforeBudgetCheck(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	state := goal.CreateGoalState("stay within the budget", time.UnixMilli(1), &goal.GoalBudget{TokenLimit: new(int64(500))})
	data, err := ai.MarshalJSON(goal.GoalChangeEntry{Operation: goal.OpCreate, Goal: &state, Timestamp: 1})
	if err != nil {
		t.Fatal(err)
	}
	sink.branchEntries = []goal.CustomEntry{{CustomType: goal.GoalCustomType, Data: data}}
	sink.streaming = true
	controller.Start("startup")
	controller.HandleCommand("resume")
	controller.AgentEnd(&agent.AgentEvent{Type: agent.AgentEnd, Messages: []ai.Message{
		&ai.AssistantMessage{StopReason: ai.StopToolUse, Usage: ai.Usage{TotalTokens: 6000000000000000000}},
		&ai.AssistantMessage{StopReason: ai.StopStop, Usage: ai.Usage{TotalTokens: 6000000000000000000}},
	}})

	view := controller.Snapshot().Goal
	if view == nil {
		t.Fatal("goal disappeared")
	}
	if view.UsedTokens != math.MaxInt64 {
		t.Errorf("round token usage = %d, want %d", view.UsedTokens, int64(math.MaxInt64))
	}
	if view.Phase != goal.PhasePaused || view.BlockedReason == nil || view.BlockedReason.Code != "budget-tokens" {
		t.Errorf("exhausted token budget did not pause the goal: %+v", view)
	}

	entries := append([]goal.CustomEntry(nil), sink.branchEntries...)
	for _, entry := range sink.emitted {
		entries = append(entries, goal.CustomEntry{CustomType: entry.CustomType, Data: entry.Data})
	}
	replayed, err := goal.FoldGoal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Goal == nil || replayed.Goal.UsedTokens != math.MaxInt64 || replayed.Goal.Phase != goal.PhasePaused {
		t.Fatalf("replay lost the exhausted usage/budget pause: %+v", replayed.Goal)
	}
}
