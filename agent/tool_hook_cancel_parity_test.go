package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 agent-loop.ts prepareToolCall catches a thrown beforeToolCall
// failure before checking the signal. Cancellation must not hide that error.
func TestBeforeToolCallFailureWinsOverCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	call := ai.ToolCall{ID: "call", Name: "probe", Arguments: json.RawMessage(`{}`)}
	outcome := RunToolCall(call, RunToolCallOptions{
		Signal: ctx,
		Tools: []AgentTool{{Name: "probe", Parameters: json.RawMessage(`{"type":"object"}`), Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
			t.Fatal("tool executed after failed hook")
			return AgentToolResult{}, nil
		}}},
		BeforeToolCall: func(*BeforeToolCallContext, context.Context) (*BeforeToolCallResult, error) {
			cancel()
			return nil, errors.New("fixture hook failed")
		},
	})
	if !outcome.IsError || len(outcome.Result.Content) != 1 || outcome.Result.Content[0].(ai.TextContent).Text != "fixture hook failed" {
		t.Fatalf("outcome = %+v, want the hook failure instead of Operation aborted", outcome)
	}
}
