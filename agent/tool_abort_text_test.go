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

// Aborting during a tool batch still answers every call: the loops no longer
// stop early, and prepareToolCall returns "Operation aborted" before validation
// and the hooks once the run is aborted, so each call gets a start event, an end
// event and a result (agent-loop.ts, v1.6.0, #9986). Whether a call that already
// started finishes or reports the abort depends on where the cancel lands.
func TestAbortedToolBatchAnswersEveryCall(t *testing.T) {
	for _, mode := range []ToolExecutionMode{ToolExecutionSequential, ToolExecutionParallel} {
		t.Run(string(mode), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := []ai.ToolCall{
				{ID: "a", Name: "tool", Arguments: json.RawMessage(`{}`)},
				{ID: "b", Name: "tool", Arguments: json.RawMessage(`{}`)},
				{ID: "c", Name: "tool", Arguments: json.RawMessage(`{}`)},
			}
			tool := &AgentTool{
				Name:       "tool",
				Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
				Execute: func(id string, _ json.RawMessage, _ context.Context, _ func(AgentToolResult)) (AgentToolResult, error) {
					cancel()
					return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "ran " + id}}}, nil
				},
			}
			config := &AgentLoopConfig{ToolExecution: mode}
			message := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m"}
			for _, call := range calls {
				message.Content = append(message.Content, call)
			}
			agentContext := &AgentContext{Tools: []AgentTool{*tool}, Messages: []ai.Message{message}}
			batch := executeToolCalls(agentContext, message, config, ctx, func(AgentEvent) error { return nil })
			if len(batch.messages) != len(calls) {
				t.Fatalf("tool results = %d, want one per call (%d)", len(batch.messages), len(calls))
			}
			seen := map[string]bool{}
			for _, result := range batch.messages {
				toolResult, ok := result.(*ai.ToolResultMessage)
				if !ok {
					t.Fatalf("result %T is not a tool result", result)
				}
				if seen[toolResult.ToolCallID] {
					t.Fatalf("duplicate result for %q", toolResult.ToolCallID)
				}
				seen[toolResult.ToolCallID] = true
				if toolResult.IsError && (len(toolResult.Content) == 0 ||
					toolResult.Content[0].(ai.TextContent).Text != "Operation aborted") {
					t.Fatalf("result for %q = %+v, want the abort error", toolResult.ToolCallID, toolResult)
				}
			}
			for _, call := range calls {
				if !seen[call.ID] {
					t.Fatalf("call %q has no tool result", call.ID)
				}
			}
		})
	}
}
