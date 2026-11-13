package coding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
)

// A goal wrap-up sent from inside set_goal must persist after the tool result.
// Upstream sendCustomMessage defers a non-trigger custom message while
// streaming to the end of the turn (pi v1.5.0, f9a26365,
// packages/coding-agent/src/core/agent-session.ts `_pendingCustomMessages`).
// Appended immediately, it sat between the assistant tool call and its result,
// and pi's replay of the session sent an orphaned tool message that
// OpenAI-compatible providers reject with a 400.
func TestGoalWrapupPersistsAfterSetGoalResult(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	round := 0
	streamFn := func(m *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		round++
		message := &ai.AssistantMessage{API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
			Content: ai.ContentList{ai.TextContent{Text: "done"}}}
		if round == 1 {
			message.StopReason = ai.StopToolUse
			message.Content = ai.ContentList{ai.ToolCall{ID: "call-1", Name: "set_goal", Arguments: json.RawMessage(`{"status":"complete"}`)}}
		}
		stream := ai.NewAssistantMessageEventStream()
		go stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, Goal: true, ConvertToLlm: ConvertToLlm,
	})
	if err != nil {
		t.Fatal(err)
	}
	var pending []func(context.Context) error
	session.Goal.SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
	session.Goal.HandleCommand("set objective")
	if len(pending) != 1 {
		t.Fatalf("round reservations = %d, want 1", len(pending))
	}
	if err := pending[0](context.Background()); err != nil {
		t.Fatal(err)
	}
	if state := session.Goal.Snapshot().Goal; state == nil || state.Phase != goal.PhaseComplete {
		t.Fatalf("goal = %+v, want complete", state)
	}

	callAt, resultAt, wrapupAt := -1, -1, -1
	for i, entry := range manager.GetBranch("") {
		if entry.Type == "custom_message" && entry.CustomType == goal.GoalEventType && callAt >= 0 && wrapupAt < 0 {
			wrapupAt = i
		}
		if entry.Type != "message" {
			continue
		}
		var probe struct {
			Role       string `json:"role"`
			ToolCallID string `json:"toolCallId"`
		}
		_ = json.Unmarshal(entry.Message, &probe)
		if probe.Role == "assistant" && callAt < 0 {
			callAt = i
		}
		if probe.Role == "toolResult" && probe.ToolCallID == "call-1" {
			resultAt = i
		}
	}
	if callAt < 0 || resultAt < 0 || wrapupAt < 0 {
		t.Fatalf("call=%d result=%d wrapup=%d", callAt, resultAt, wrapupAt)
	}
	if callAt >= resultAt || resultAt >= wrapupAt {
		t.Fatalf("order call=%d result=%d wrapup=%d, want the wrap-up after the tool result", callAt, resultAt, wrapupAt)
	}
}
