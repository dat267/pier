package coding

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
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
	result, err := tool.Execute(context.Background(), "id", json.RawMessage(params), func(agent.AgentToolResult) {})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if text, ok := result.Content[0].(ai.TextContent); ok {
		return text.Text
	}
	return ""
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

func TestGoalControllerBuffersNoticesUntilUIHooks(t *testing.T) {
	version := 1
	data, _ := json.Marshal(goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal:      &goal.GoalSnapshot{Version: &version, ID: "g1", Revision: 1, Objective: "obj", Phase: goal.PhaseActive, CreatedAt: 1, UpdatedAt: 1},
		Timestamp: 1,
	})
	sink := &fakeGoalSink{branchEntries: []goal.CustomEntry{{CustomType: goal.GoalCustomType, Data: data}}}
	controller := newGoalController(sink)

	controller.Start("startup")
	if len(sink.notifies) != 0 {
		t.Fatalf("notice delivered before hooks: %v", sink.notifies)
	}
	controller.SetUIHooks(func(message string, level string) { sink.notifies = append(sink.notifies, message) })
	if len(sink.notifies) != 1 || !strings.Contains(sink.notifies[0], "Goal restored") {
		t.Fatalf("buffered notice = %v", sink.notifies)
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
	controller := newGoalController(sink)
	tool := findGoalTool(t, controller, "set_goal")
	call := func(params string) (recovered any) {
		defer func() { recovered = recover() }()
		_, _ = tool.Execute(context.Background(), "id", json.RawMessage(params), func(agent.AgentToolResult) {})
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
