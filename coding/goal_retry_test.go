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

func TestGoalControllerRetryableFailureDoesNotPause(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
	sink.entries = nil
	sink.retry = true
	assistant := completedAssistant()
	assistant.StopReason = ai.StopError
	assistant.ErrorMessage = new("server overloaded")
	controller.AgentEnd(goalAgentEndEvent(assistant))

	if goalPausedWithReason(sink.entries, "run-error") {
		t.Fatalf("a retryable failure paused the goal: %+v", sink.entries)
	}
	if snapshot := controller.Snapshot(); snapshot.Goal == nil || snapshot.Goal.Phase != goal.PhaseActive {
		t.Fatalf("goal must stay active through a retry: %+v", snapshot.Goal)
	}
	if len(sink.messages) != 0 {
		t.Fatalf("a retry queued a duplicate goal round: %+v", sink.messages)
	}
	if len(sink.notifies) != 0 {
		t.Fatalf("a retry emitted a pause notice: %v", sink.notifies)
	}
}

// The session retries the HTTP/2 failure and the retry completes the same goal
// round. The goal must remain active when the controller receives willRetry.
func TestAgentSessionRetryableFailureRecoveryKeepsGoalActive(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	var session *AgentSession
	var calls int
	streamFn := func(requestModel *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		call := calls
		if call == 1 {
			if _, err := session.control.Tools["set_goal"].Tool.Execute("id", json.RawMessage(`{"objective":"finish it"}`), context.Background(), func(agent.AgentToolResult) {}); err != nil {
				t.Errorf("create goal: %v", err)
			}
		}
		if call == 4 {
			if _, err := session.control.Tools["set_goal"].Tool.Execute("id", json.RawMessage(`{"status":"complete"}`), context.Background(), func(agent.AgentToolResult) {}); err != nil {
				t.Errorf("complete goal: %v", err)
			}
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID}
			if call == 2 || call == 3 {
				errorText := `Post "https://api.commandcode.ai/provider/v1/chat/completions": stream error: stream ID 11; PROTOCOL_ERROR; received from peer`
				message.StopReason = ai.StopError
				message.ErrorMessage = &errorText
			} else {
				message.Content = ai.ContentList{ai.TextContent{Text: "done"}}
				message.StopReason = ai.StopStop
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		}()
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn,
		Tools: []agent.AgentTool{testTool("read")}, ConvertToLlm: ConvertToLlm, Goal: true,
		Settings: SessionSettings{Retry: &ai.RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelayMS: 1}, Compaction: DefaultCompactionSettings},
		Control:  &AgentSessionControl{autoRetry: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	sawRetryableFailure := false
	session.Subscribe(func(event *SessionEvent) {
		if event.Type != SessionAgentEnd || !event.WillRetry || event.Agent == nil {
			return
		}
		for _, message := range event.Agent.Messages {
			assistant, ok := message.(*ai.AssistantMessage)
			if ok && assistant.StopReason == ai.StopError && assistant.ErrorMessage != nil && strings.Contains(*assistant.ErrorMessage, "PROTOCOL_ERROR") {
				sawRetryableFailure = true
				if snapshot := session.Goal.Snapshot(); snapshot.Goal == nil || snapshot.Goal.Phase != goal.PhaseActive {
					t.Errorf("retryable failure paused the goal: %+v", snapshot.Goal)
				}
			}
		}
	})

	if err := session.Prompt(context.Background(), "start", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("model calls = %d, want initial round, two failed attempts, and successful retry", calls)
	}
	if !sawRetryableFailure {
		t.Fatal("did not observe the HTTP/2 failure being scheduled for retry")
	}
	if snapshot := session.Goal.Snapshot(); snapshot.Goal == nil || snapshot.Goal.Phase != goal.PhaseComplete {
		t.Fatalf("recovered round did not complete the goal: %+v", snapshot.Goal)
	}
}

func TestGoalControllerFinalFailureStillPauses(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"do it"}`)
	sink.entries = nil
	sink.retry = false
	assistant := completedAssistant()
	assistant.StopReason = ai.StopError
	assistant.ErrorMessage = new("bad request")
	controller.AgentEnd(goalAgentEndEvent(assistant))

	if !goalPausedWithReason(sink.entries, "run-error") {
		t.Fatalf("a final failure did not pause: %+v", sink.entries)
	}
	if len(sink.messages) != 1 || sink.messages[0].triggerTurn || !strings.Contains(sink.messages[0].content, "<goal_paused>") {
		t.Fatalf("final failure message = %+v", sink.messages)
	}
	if len(sink.notifies) != 1 || !strings.Contains(sink.notifies[0], "Goal paused") {
		t.Fatalf("final failure notice = %v", sink.notifies)
	}
}
