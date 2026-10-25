package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 agent-loop.ts finalizeExecutedToolCall merges details with ??,
// so a null/undefined after-hook value keeps the executed result's details.
func TestAfterToolCallNullDetailsKeepExecutedDetails(t *testing.T) {
	for _, details := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage(" \nnull\t")} {
		call := ai.ToolCall{ID: "call", Name: "probe", Arguments: json.RawMessage(`{}`)}
		outcome := RunToolCall(call, RunToolCallOptions{
			Tools: []AgentTool{{Name: "probe", Parameters: json.RawMessage(`{"type":"object"}`), Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
				return AgentToolResult{Details: json.RawMessage(`{"original":true}`)}, nil
			}}},
			AfterToolCall: func(*AfterToolCallContext, context.Context) (*AfterToolCallResult, error) {
				return &AfterToolCallResult{HasDetails: true, Details: details}, nil
			},
		})
		if outcome.IsError || string(outcome.Result.Details) != `{"original":true}` {
			t.Errorf("after details=%q: outcome=%+v, want original details", details, outcome)
		}
	}
}
