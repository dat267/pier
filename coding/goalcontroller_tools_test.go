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
	snapshot := session.GoalController().Snapshot()
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
			if snapshot := session.GoalController().Snapshot().Goal; snapshot != nil {
				_ = snapshot
				params := json.RawMessage(`{"status":"complete"}`)
				tool := session.control.Tools["set_goal"].Tool
				_, _ = tool.Execute(context.Background(), "complete", params, func(agent.AgentToolResult) {})
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
	if _, err := create.Execute(context.Background(), "id", json.RawMessage(`{"objective":"do it"}`), func(agent.AgentToolResult) {}); err != nil {
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
	if phase := session.GoalController().Snapshot().Goal.Phase; phase != goal.PhaseComplete {
		t.Fatalf("goal phase = %s", phase)
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
			_, _ = session.GetToolDefinition("set_goal").Execute(context.Background(), "complete", json.RawMessage(`{"status":"complete"}`), nil)
		}
		return textReplyStream(m, "done")
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
	session.GoalController().SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
	session.FollowUp(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "after the goal"}})
	session.GoalController().HandleCommand("set objective")
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
				_, _ = session.GetToolDefinition("set_goal").Execute(context.Background(), "complete", json.RawMessage(`{"status":"complete"}`), nil)
				return textReplyStream(model, "done")
			}
			var err error
			session, err = NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, Goal: true, ConvertToLlm: ConvertToLlm})
			if err != nil {
				t.Fatal(err)
			}
			if commandStartsRound {
				session.GoalController().HandleCommand("set finish the objective")
			} else {
				if _, err := session.GetToolDefinition("set_goal").Execute(context.Background(), "create", json.RawMessage(`{"objective":"finish the objective"}`), nil); err != nil {
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
			result, err := tool.Execute(context.Background(), "call", json.RawMessage(params), nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != fixture.reject {
				t.Fatalf("IsError = %v, want %v: %+v", result.IsError, fixture.reject, result.Content)
			}
			snapshot := session.GoalController().Snapshot().Goal
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
	session.GoalController().SetStartWork(func(func(context.Context) error) {})

	if err := session.Prompt(context.Background(), "before the goal", nil); err != nil {
		t.Fatal(err)
	}
	forks := session.GetUserMessagesForForking()
	if len(forks) != 1 {
		t.Fatalf("fork targets = %+v", forks)
	}
	session.GoalController().HandleCommand("set ship the release")
	if session.GoalController().Snapshot().Goal == nil {
		t.Fatal("the goal was not set")
	}

	// Back to before the goal existed: the branch holds no goal now.
	if _, err := session.NavigateTree(context.Background(), forks[0].EntryID, NavigateTreeOptions{}); err != nil {
		t.Fatal(err)
	}
	if goal := session.GoalController().Snapshot().Goal; goal != nil {
		t.Fatalf("after navigating to before the goal, the controller still holds %q", goal.Objective)
	}

	// A command now must persist against the branch's goal state, and the
	// branch must replay without a corruption warning.
	session.GoalController().HandleCommand("set a different objective")
	var warnings []string
	session.GoalController().SetUIHooks(func(message, level string) {
		if level == "warning" && strings.Contains(message, "corrupt") {
			warnings = append(warnings, message)
		}
	})
	session.GoalController().Start("startup")
	if len(warnings) > 0 {
		t.Fatalf("the branch does not replay after navigating: %v", warnings)
	}
	if goal := session.GoalController().Snapshot().Goal; goal == nil || goal.Objective != "a different objective" {
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
