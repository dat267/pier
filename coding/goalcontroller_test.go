package coding

// Mirrors ~/.pi/agent/extensions/goal/index.test.ts: the host-glue smoke test
// for the controller (effect application, session_start replay, tool closures,
// provider-error handling, tool exposure).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/goal"
)

type recordedEntry struct {
	entryType string
	data      map[string]any
}

type recordedMessage struct {
	customType  string
	content     string
	triggerTurn bool
}

type fakeGoalSink struct {
	entries       []recordedEntry
	messages      []recordedMessage
	notifies      []string
	notifyLevels  []string
	statuses      int
	active        []string
	confirms      int
	confirmResult bool
}

func (f *fakeGoalSink) AppendGoalEntry(entryType string, data json.RawMessage) {
	var decoded map[string]any
	_ = json.Unmarshal(data, &decoded)
	f.entries = append(f.entries, recordedEntry{entryType: entryType, data: decoded})
}

func (f *fakeGoalSink) SendGoalMessage(customType string, content string, display bool, details json.RawMessage, triggerTurn bool) {
	f.messages = append(f.messages, recordedMessage{customType: customType, content: content, triggerTurn: triggerTurn})
}

func (f *fakeGoalSink) NotifyGoal(message string, level string) {
	f.notifies = append(f.notifies, message)
	f.notifyLevels = append(f.notifyLevels, level)
}

func (f *fakeGoalSink) RenderGoalStatus() { f.statuses++ }

func (f *fakeGoalSink) GoalActiveTools() []string {
	return append([]string{}, f.active...)
}

func (f *fakeGoalSink) SetGoalActiveTools(names []string) {
	f.active = append([]string{}, names...)
}

func (f *fakeGoalSink) ConfirmGoalReplace(title string, message string, onAnswer func(confirmed bool)) {
	f.confirms++
	onAnswer(f.confirmResult)
}

func newGoalControllerTest(t *testing.T) (*GoalController, *fakeGoalSink) {
	t.Helper()
	sink := &fakeGoalSink{}
	controller := NewGoalController(goal.NewGoalMachine(), sink)
	return controller, sink
}

func findTool(t *testing.T, controller *GoalController, name string) agent.AgentTool {
	t.Helper()
	for _, tool := range controller.GoalTools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return agent.AgentTool{}
}

func goalToolText(t *testing.T, controller *GoalController, name string, params string) string {
	t.Helper()
	tool := findTool(t, controller, name)
	result, err := tool.Execute("id", json.RawMessage(params), context.Background(), func(agent.AgentToolResult) {})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if text, ok := result.Content[0].(ai.TextContent); ok {
		return text.Text
	}
	return ""
}

func TestGoalControllerRegistersThreeTools(t *testing.T) {
	controller, _ := newGoalControllerTest(t)
	names := []string{}
	for _, tool := range controller.GoalTools() {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "get_goal,create_goal,update_goal" {
		t.Fatalf("tools = %v", names)
	}
}

func TestGoalControllerSessionStartReplaysAndExposesGoal(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	entry := goal.GoalChangeEntry{
		Operation: goal.OpCreate,
		Goal:      &goal.GoalSnapshot{Version: goalIntPtr(1), ID: "g1", Revision: 1, Objective: "obj", Phase: goal.PhaseActive, CreatedAt: 1, UpdatedAt: 1},
		Timestamp: 1,
	}
	data, _ := json.Marshal(entry)
	controller.SessionStart("startup", []goal.CustomEntry{{CustomType: goal.GoalCustomType, Data: data}})

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

func TestGoalControllerAgentSettledQueuesRound(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	sink.entries = nil
	controller.AgentSettled(false)
	if len(sink.messages) != 1 || !strings.Contains(sink.messages[0].content, "<goal_round>") {
		t.Fatalf("messages = %+v", sink.messages)
	}
	if len(sink.entries) != 0 {
		t.Fatal("turn card admitted at settle")
	}
}

func TestGoalControllerPermanentProviderErrors(t *testing.T) {
	cases := map[int]string{401: "API key", 402: "credits"}
	for status, want := range cases {
		controller, sink := newGoalControllerTest(t)
		goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
		sink.entries = nil
		sink.notifies = nil
		controller.ProviderResponse(status, nil)
		controller.AgentSettled(false)

		if len(sink.messages) != 0 {
			t.Fatalf("status %d queued a round", status)
		}
		found := false
		for _, entry := range sink.entries {
			if entry.data["operation"] == "pause" {
				found = true
			}
		}
		if !found {
			t.Fatalf("status %d did not pause", status)
		}
		notice := sink.notifies[len(sink.notifies)-1]
		if !strings.Contains(notice, "Goal paused") || !strings.Contains(notice, want) {
			t.Fatalf("status %d notice = %q", status, notice)
		}
	}
}

func TestGoalController429RetriesOnce(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	sink.entries = nil
	sink.notifies = nil
	retryAfter := int64(2000)
	controller.ProviderResponse(429, &retryAfter)
	controller.AgentSettled(false)
	if len(sink.entries) != 0 {
		t.Fatal("paused on a transient error")
	}
	if len(sink.notifies) == 0 || !strings.Contains(sink.notifies[len(sink.notifies)-1], "retrying in 30s (attempt 1/3)") {
		t.Fatalf("notifies = %v", sink.notifies)
	}
}

func TestGoalControllerAgentEndNoGoalProducesNoEffects(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	controller.AgentEnd(false)
	if len(sink.entries) != 0 || len(sink.messages) != 0 {
		t.Fatal("agent_end with no goal wrote")
	}
}

func TestGoalControllerToolExposure(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	controller.SessionStart("startup", nil)
	if strings.Join(sink.active, ",") != "create_goal" {
		t.Fatalf("active = %v", sink.active)
	}
	goalToolText(t, controller, "create_goal", `{"objective":"do it"}`)
	if strings.Join(sink.active, ",") != "create_goal,get_goal,update_goal" {
		t.Fatalf("active = %v", sink.active)
	}
	text := goalToolText(t, controller, "get_goal", "{}")
	parsed := struct {
		Goal struct {
			ID       string `json:"id"`
			Revision int    `json:"revision"`
		} `json:"goal"`
	}{}
	_ = json.Unmarshal([]byte(text), &parsed)
	goalToolText(t, controller, "update_goal", `{"goal_id":"`+parsed.Goal.ID+`","revision":`+itoaTest(parsed.Goal.Revision)+`,"action":"complete"}`)
	if strings.Join(sink.active, ",") != "create_goal" {
		t.Fatalf("active = %v", sink.active)
	}
}

func TestGoalControllerCommandReplaceConfirm(t *testing.T) {
	sink := &fakeGoalSink{confirmResult: true}
	controller := NewGoalController(goal.NewGoalMachine(), sink)
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
	controller = NewGoalController(goal.NewGoalMachine(), sink)
	goalToolText(t, controller, "create_goal", `{"objective":"first"}`)
	sink.entries = nil
	controller.HandleCommand("set second")
	if len(sink.entries) != 0 {
		t.Fatalf("declined replace wrote %+v", sink.entries)
	}
}

func goalIntPtr(value int) *int { return &value }

func itoaTest(value int) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
