package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/dat267/pier/ai"
)

// Cancelling a tool call renders "Operation aborted", never Go's transport
// text. Upstream tools take an AbortSignal and cannot produce a context error,
// so a canceled call is the same abort the loop reports before it starts work
// (agent-loop.ts createErrorToolResult("Operation aborted"), D256).
func TestCanceledToolCallReportsOperationAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, toolErr := range []error{
		context.Canceled,
		fmt.Errorf("mcp server connect: %w", context.Canceled),
		context.DeadlineExceeded,
	} {
		canceled := false
		tool := &AgentTool{
			Name: "tool",
			Execute: func(_ string, _ json.RawMessage, _ context.Context, _ func(AgentToolResult)) (AgentToolResult, error) {
				if !canceled {
					canceled = true
					cancel()
				}
				return AgentToolResult{}, toolErr
			},
		}
		prepared := &preparedToolCallValue{toolCall: ai.ToolCall{ID: "tc", Name: "tool"}, tool: tool}
		executed := executePreparedToolCall(prepared, ctx, func(AgentEvent) error { return nil })
		if !executed.isError {
			t.Fatalf("%v: canceled tool call did not fail", toolErr)
		}
		if got := executed.result.Content[0].(ai.TextContent).Text; got != "Operation aborted" {
			t.Fatalf("%v: tool result = %q, want %q", toolErr, got, "Operation aborted")
		}
	}
}

// A tool's own failure is kept as-is even though the run is being canceled
// around it: upstream reports the tool's error, not an abort.
func TestToolFailureKeepsItsMessageWhileTheRunIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()
	tool := &AgentTool{
		Name: "tool",
		Execute: func(_ string, _ json.RawMessage, _ context.Context, _ func(AgentToolResult)) (AgentToolResult, error) {
			return AgentToolResult{}, errors.New("Path not found: /tmp/x")
		},
	}
	prepared := &preparedToolCallValue{toolCall: ai.ToolCall{ID: "tc", Name: "tool"}, tool: tool}
	executed := executePreparedToolCall(prepared, ctx, func(AgentEvent) error { return nil })
	if got := executed.result.Content[0].(ai.TextContent).Text; got != "Path not found: /tmp/x" {
		t.Fatalf("tool result = %q", got)
	}
}
