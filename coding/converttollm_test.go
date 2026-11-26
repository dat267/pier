package coding

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/goal"
)

// A goal lifecycle event is appended while a tool executes, so it lands in the
// session between the assistant tool call and its result. Providers require the
// tool result to follow the assistant tool call directly; ConvertToLlm must
// keep the custom note out of that gap.
func TestConvertToLlmDefersCustomMessageBetweenToolCallAndResult(t *testing.T) {
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "please update the goal"}}
	assistant := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.ToolCall{ID: "c1", Name: "set_goal", Arguments: json.RawMessage(`{}`)}},
		StopReason: ai.StopToolUse,
	}
	goalEvent := goalEventMessage(goal.GoalEventType, "<goal_complete>done", false, nil)
	result := &ai.ToolResultMessage{
		ToolCallID: "c1", ToolName: "set_goal",
		Content: ai.UserContentList{ai.TextContent{Text: "goal updated"}},
	}

	converted := ConvertToLlm([]ai.Message{user, assistant, goalEvent, result})
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "p"}
	transformed := ai.TransformMessages(converted, model, nil)

	assertToolResultsFollowToolCalls(t, transformed)
	if !containsUserText(transformed, "<goal_complete>") {
		t.Fatal("the goal event must stay in context, after the tool result")
	}
}

// The note is held only to clear the ONE gap it landed in. A later turn that never
// answers the call must not carry the note forward: pendingToolCalls is cleared by
// the next assistant message, so a note still held at that point was being flushed
// only at the very end of the transcript, after a user turn it should have preceded.
func TestConvertToLlmDoesNotCarryAHeldNoteAcrossATurn(t *testing.T) {
	call := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.ToolCall{ID: "c1", Name: "t", Arguments: json.RawMessage(`{}`)}},
		StopReason: ai.StopToolUse,
	}
	note := &ai.CustomMessage{Role: RoleCustom, Timestamp: 1,
		Content: json.RawMessage(`{"customType":"note","content":"note text"}`)}
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "next turn"}}

	converted := ConvertToLlm([]ai.Message{call, note, user})
	var order []string
	for _, m := range converted {
		switch v := m.(type) {
		case *ai.UserMessage:
			// The custom note projects onto a user message, so identify it by its
			// text rather than by the Go type.
			text := v.Content.Text
			for _, b := range v.Content.Blocks {
				if tb, ok := b.(ai.TextContent); ok {
					text += tb.Text
				}
			}
			if text == "note text" {
				order = append(order, "note")
			} else {
				order = append(order, "user:"+text)
			}
		case *ai.AssistantMessage:
			order = append(order, "assistant")
		}
	}
	want := []string{"assistant", "note", "user:next turn"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v (the held note crossed a turn boundary)", order, want)
		}
	}
}

// assertToolResultsFollowToolCalls walks the wire sequence the way an
// OpenAI-compatible provider does: a tool message may only follow an assistant
// message that declared its tool_call_id, or another tool message from that
// same batch.
func assertToolResultsFollowToolCalls(t *testing.T, messages []ai.Message) {
	t.Helper()
	declared := map[string]bool{}
	for i, message := range messages {
		switch m := message.(type) {
		case *ai.AssistantMessage:
			declared = map[string]bool{}
			for _, block := range m.Content {
				if call, ok := block.(ai.ToolCall); ok {
					declared[call.ID] = true
				}
			}
		case *ai.ToolResultMessage:
			if !declared[m.ToolCallID] {
				t.Fatalf("message %d: tool result %s has no preceding tool_call", i, m.ToolCallID)
			}
		case *ai.SystemMessage:
			// A system message is transparent to tool-call accounting upstream.
		default:
			declared = map[string]bool{}
		}
	}
}

func containsUserText(messages []ai.Message, needle string) bool {
	for _, message := range messages {
		user, ok := message.(*ai.UserMessage)
		if !ok {
			continue
		}
		for _, block := range user.Content.Blocks {
			if text, ok := block.(ai.TextContent); ok && strings.Contains(text.Text, needle) {
				return true
			}
		}
	}
	return false
}

// A note held behind a call that IS answered later must still land after that
// result, not before it: providers require the result to follow its call.
func TestConvertToLlmKeepsAHeldNoteAfterItsAnsweringResult(t *testing.T) {
	call := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.ToolCall{ID: "c1", Name: "t", Arguments: json.RawMessage(`{}`)}},
		StopReason: ai.StopToolUse,
	}
	note := &ai.CustomMessage{Role: RoleCustom, Timestamp: 1,
		Content: json.RawMessage(`{"customType":"note","content":"note text"}`)}
	result := &ai.ToolResultMessage{ToolCallID: "c1", ToolName: "t",
		Content: ai.UserContentList{ai.TextContent{Text: "ok"}}}

	converted := ConvertToLlm([]ai.Message{call, note, result})
	var order []string
	for _, m := range converted {
		switch v := m.(type) {
		case *ai.ToolResultMessage:
			order = append(order, "result")
		case *ai.UserMessage:
			text := v.Content.Text
			for _, b := range v.Content.Blocks {
				if tb, ok := b.(ai.TextContent); ok {
					text += tb.Text
				}
			}
			if text == "note text" {
				order = append(order, "note")
			}
		case *ai.AssistantMessage:
			order = append(order, "assistant")
		}
	}
	want := []string{"assistant", "result", "note"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}
