package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
)

// A paused goal resumed from an idle session starts a real run. Before the fix
// the resume only queued the round as a follow-up, and nothing drained it: the
// stream function was never called.
func TestGoalResumeFromIdleStartsARun(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	var session *AgentSession
	var prompts atomic.Int64
	var roundSeen atomic.Bool
	streamFn := func(requestModel *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		prompts.Add(1)
		for _, message := range transcript.Messages {
			if user, ok := message.(*ai.UserMessage); ok && strings.Contains(contentTextJoinedNoSep(user.Content), "<goal_round>") {
				roundSeen.Store(true)
			}
		}
		// Stop the round loop: complete the goal from this round, then answer.
		if view := session.Goal.Snapshot().Goal; view != nil {
			_, _ = session.control.Tools["update_goal"].Tool.Execute("complete",
				json.RawMessage(fmt.Sprintf(`{"goal_id":%q,"revision":%d,"action":"complete"}`, view.ID, view.Revision)),
				context.Background(), func(agent.AgentToolResult) {})
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop,
			}})
		}()
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn,
		Tools: []agent.AgentTool{testTool("read")}, ConvertToLlm: ConvertToLlm, Goal: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := session.control.Tools["create_goal"].Tool.Execute("create",
		json.RawMessage(`{"objective":"finish it"}`), context.Background(), func(agent.AgentToolResult) {}); err != nil {
		t.Fatal(err)
	}
	session.Goal.HandleCommand("pause")
	if !session.IsIdle() {
		t.Fatal("expected an idle session after pause")
	}

	session.Goal.HandleCommand("resume")
	if prompts.Load() == 0 {
		t.Fatal("resume did not start a run")
	}
	if !roundSeen.Load() {
		t.Fatal("the round prompt did not reach the model context")
	}
	if phase := session.Goal.Snapshot().Goal.Phase; phase != goal.PhaseComplete {
		t.Fatalf("phase = %q, want complete", phase)
	}
}

// A goal crosses two round boundaries from one prompt and the run loop
// continues through both. The run that completes the goal is not admitted
// (upstream: "a finishing run is not work done 'for' the goal"), so the two
// work rounds are admitted once each and no fourth round starts.
func TestGoalRoundTransitionAdmitsEachRoundOnce(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	var session *AgentSession
	var prompts atomic.Int64
	streamFn := func(requestModel *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		switch prompts.Add(1) {
		case 1:
			_, _ = session.control.Tools["create_goal"].Tool.Execute("create",
				json.RawMessage(`{"objective":"finish it"}`), context.Background(), func(agent.AgentToolResult) {})
		case 3:
			view := session.Goal.Snapshot().Goal
			_, _ = session.control.Tools["update_goal"].Tool.Execute("complete",
				json.RawMessage(fmt.Sprintf(`{"goal_id":%q,"revision":%d,"action":"complete"}`, view.ID, view.Revision)),
				context.Background(), func(agent.AgentToolResult) {})
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "working"}}, StopReason: ai.StopStop,
			}})
		}()
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn,
		Tools: []agent.AgentTool{testTool("read")}, ConvertToLlm: ConvertToLlm, Goal: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := session.Prompt(context.Background(), "start", nil); err != nil {
		t.Fatal(err)
	}
	if calls := prompts.Load(); calls != 3 {
		t.Fatalf("model calls = %d, want 3", calls)
	}
	snapshot := session.Goal.Snapshot()
	if snapshot.Goal.Phase != goal.PhaseComplete {
		t.Fatalf("phase = %q, want complete", snapshot.Goal.Phase)
	}
	if snapshot.Goal.TurnsStarted != 2 {
		t.Fatalf("turnsStarted = %d, want 2", snapshot.Goal.TurnsStarted)
	}
	turns := 0
	for _, entry := range manager.GetBranch("") {
		if entry.Type == "custom" && entry.CustomType == goal.GoalTurnType {
			turns++
		}
	}
	if turns != 2 {
		t.Fatalf("admitted turns = %d, want 2", turns)
	}
}
