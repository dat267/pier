package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream agent-loop.ts carries execute duration on AgentToolCallOutcome when tool ran.
func TestRunToolCallRecordsDurationOnlyForExecutedTools(t *testing.T) {
	call := ai.ToolCall{ID: "tc", Name: "run", Arguments: json.RawMessage(`{}`)}
	assistant := createAssistantMessage(ai.ContentList{call}, ai.StopToolUse)
	tool := AgentTool{
		Name: "run", Parameters: json.RawMessage(`{"type":"object"}`),
		Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "done"}}}, nil
		},
	}
	executed := RunToolCall(call, RunToolCallOptions{Tools: []AgentTool{tool}, AssistantMessage: assistant})
	if executed.DurationMs == nil || *executed.DurationMs < 0 {
		t.Fatalf("executed tool duration = %v, want non-nil nonnegative duration", executed.DurationMs)
	}
	missing := RunToolCall(call, RunToolCallOptions{AssistantMessage: assistant})
	if missing.DurationMs != nil {
		t.Fatalf("unexecuted tool duration = %v, want nil", *missing.DurationMs)
	}
}

// Upstream agent-loop.ts retains execute duration when afterToolCall turns result into an error.
func TestRunToolCallAfterHookFailureKeepsExecutionDuration(t *testing.T) {
	call := ai.ToolCall{ID: "tc", Name: "run", Arguments: json.RawMessage(`{}`)}
	assistant := createAssistantMessage(ai.ContentList{call}, ai.StopToolUse)
	tool := AgentTool{
		Name: "run", Parameters: json.RawMessage(`{"type":"object"}`),
		Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "done"}}}, nil
		},
	}
	outcome := RunToolCall(call, RunToolCallOptions{
		Tools: []AgentTool{tool}, AssistantMessage: assistant,
		AfterToolCall: func(*AfterToolCallContext, context.Context) (*AfterToolCallResult, error) {
			return nil, errors.New("after hook failed")
		},
	})
	if !outcome.IsError || outcome.DurationMs == nil {
		t.Fatalf("after-hook failure outcome = %+v, want error with executed duration", outcome)
	}
}
