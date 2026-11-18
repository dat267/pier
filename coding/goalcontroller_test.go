package coding

import (
	"context"
	"encoding/json"
	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
	"strings"
	"testing"
)

// Mirrors ~/.pi/agent/extensions/goal/index.test.ts: the host-glue smoke test
// for the controller (effect application, session_start replay, tool closures,
// tool exposure, and the continuation/error/abort settle outcomes).

func goalControllerForSink(sink *fakeGoalSink) *GoalController {
	controller := newGoalController(nil, sink)
	controller.SetUIHooks(func(message string, level string) { sink.notifies = append(sink.notifies, message) })
	return controller
}

func goalAgentEndEvent(assistant *ai.AssistantMessage) *agent.AgentEvent {
	return &agent.AgentEvent{Type: agent.AgentEnd, Messages: []ai.Message{assistant}}
}

func completedAssistant() *ai.AssistantMessage {
	return &ai.AssistantMessage{StopReason: ai.StopStop}
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
