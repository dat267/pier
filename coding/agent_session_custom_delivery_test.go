package coding

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 core/agent-session.ts sendCustomMessage queues a
// triggering message during a run. An agent_end listener must not merely
// append it to history, leaving its requested turn unserved.
func TestSendMessageDuringRunTriggersQueuedTurn(t *testing.T) {
	for _, deliverAs := range []string{"steer", "followUp"} {
		t.Run(deliverAs, func(t *testing.T) {
			var calls atomic.Int64
			session := newPromptSession(t, &ai.Model{ID: "m", Provider: "anthropic", API: ai.APIAnthropicMessages, ContextWindow: 100000}, recordingStreamFn(t, &calls))
			queued := false
			session.Subscribe(func(event *SessionEvent) {
				if event.Type == SessionAgentEnd && !queued {
					queued = true
					err := session.SendMessage(context.Background(), goalEventMessage("note", "do the next task", false, nil), &SendMessageOptions{DeliverAs: deliverAs})
					if err != nil {
						t.Error(err)
					}
				}
			})
			if err := session.Prompt(context.Background(), "start", nil); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 {
				t.Fatalf("model requests = %d; want 2, including the queued custom turn", calls.Load())
			}
		})
	}
}

// Reference: pi v1.4.0 core/agent-session.ts sendCustomMessage's idle
// non-triggering branch persists the
// message before publishing message_start/message_end, so UI listeners can
// display it without waiting for another run or rebuilding the transcript.
func TestContextOnlyCustomMessagePublishesLifecycle(t *testing.T) {
	var calls atomic.Int64
	session := newPromptSession(t, &ai.Model{ID: "m", Provider: "anthropic", API: ai.APIAnthropicMessages}, recordingStreamFn(t, &calls))
	custom := goalEventMessage("note", "visible note", true, nil)
	var events []SessionEventType
	session.Subscribe(func(event *SessionEvent) {
		if event.Agent == nil || event.Agent.Message != custom {
			return
		}
		entries := session.Sessions.GetEntries()
		if len(entries) == 0 || entries[len(entries)-1].Type != "custom_message" {
			t.Error("custom message lifecycle was published before persistence")
		}
		events = append(events, event.Type)
	})
	if err := session.SendMessage(context.Background(), custom, &SendMessageOptions{TriggerTurn: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0] != SessionMessageStart || events[1] != SessionMessageEnd {
		t.Fatalf("custom message events = %v; want [message_start message_end]", events)
	}
	if calls.Load() != 0 {
		t.Fatal("context-only message started a model request")
	}
}

// Reference: pi v1.4.0 core/agent-session.ts _handleAgentEvent flushes
// context-only custom messages at turn_end, after tool results and before
// the next request. Waiting until the entire run ends hides them from the model.
func TestContextOnlyCustomMessageReachesNextTurn(t *testing.T) {
	model := &ai.Model{ID: "m", Provider: "anthropic", API: ai.APIAnthropicMessages, ContextWindow: 100000}
	response := createAssistantMessageT("")
	response.StopReason = ai.StopToolUse
	response.Content = ai.ContentList{ai.ToolCall{ID: "read-1", Name: "read", Arguments: json.RawMessage(`{}`)}}
	session := newPromptSession(t, model, mockSessionStreamFn(response, createAssistantMessageT("done")))
	custom := goalEventMessage("note", "important context for the next request", false, nil)
	turns := 0
	session.Subscribe(func(event *SessionEvent) {
		if event.Type == SessionTurnEnd && turns == 1 {
			if err := session.SendMessage(context.Background(), custom, &SendMessageOptions{TriggerTurn: boolPtr(false)}); err != nil {
				t.Error(err)
			}
		}
		if event.Type == SessionTurnStart {
			turns++
			if turns == 2 {
				messages := session.Messages()
				if messages[len(messages)-1] != custom {
					t.Errorf("next turn starts without the queued custom message; last role = %s", ai.RoleOf(messages[len(messages)-1]))
				}
			}
		}
	})
	if err := session.Prompt(context.Background(), "start", nil); err != nil {
		t.Fatal(err)
	}
	if turns != 2 {
		t.Fatalf("turns = %d; want 2", turns)
	}
}
