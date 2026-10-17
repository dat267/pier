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
