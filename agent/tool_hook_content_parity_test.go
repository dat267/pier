package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 agent-loop.ts merges content with ??, not a presence-only check.
// A null/undefined override keeps content; a present empty array replaces it.
func TestAfterToolCallNullContentKeepsExecutedContent(t *testing.T) {
	for _, content := range [][]ai.Content{nil, {}} {
		call := ai.ToolCall{ID: "call", Name: "probe", Arguments: json.RawMessage(`{}`)}
		outcome := RunToolCall(call, RunToolCallOptions{
			Tools: []AgentTool{{Name: "probe", Parameters: json.RawMessage(`{"type":"object"}`), Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
				return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "original"}}}, nil
			}}},
			AfterToolCall: func(*AfterToolCallContext, context.Context) (*AfterToolCallResult, error) {
				return &AfterToolCallResult{HasContent: true, Content: content}, nil
			},
		})
		if content == nil {
			if len(outcome.Result.Content) != 1 || outcome.Result.Content[0].(ai.TextContent).Text != "original" {
				t.Fatalf("null override discarded executed content: %+v", outcome)
			}
		} else if outcome.Result.Content == nil || len(outcome.Result.Content) != 0 {
			t.Fatalf("empty array did not replace content: %+v", outcome)
		}
	}
}
