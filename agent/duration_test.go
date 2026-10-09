package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// #10549: a tool call's execution time is measured with a monotonic clock and reaches both the
// tool result message (which is persisted, so a replayed result keeps it) and the
// tool_execution_end event.
func TestToolCallDurationIsRecordedAndPublished(t *testing.T) {
	const sleep = 20 * time.Millisecond
	tool := &AgentTool{
		Name: "slow",
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
			time.Sleep(sleep)
			return AgentToolResult{}, nil
		},
	}
	prepared := &preparedToolCallValue{
		toolCall: ai.ToolCall{ID: "call-1", Name: "slow", Arguments: json.RawMessage(`{}`)},
		tool:     tool,
		args:     json.RawMessage(`{}`),
	}
	executed := executePreparedToolCall(prepared, context.Background(), func(AgentEvent) error { return nil })
	if executed.durationMS < sleep.Milliseconds() {
		t.Fatalf("durationMS = %d, want at least the %d ms the tool slept", executed.durationMS, sleep.Milliseconds())
	}

	finalized := finalizedToolCallOutcome{
		toolCall: prepared.toolCall, result: executed.result, isError: executed.isError,
		durationMS: &executed.durationMS,
	}
	message := createToolResultMessage(finalized)
	if message.DurationMs == nil || *message.DurationMs != executed.durationMS {
		t.Fatalf("tool result message duration = %v, want %d", message.DurationMs, executed.durationMS)
	}

	var events []AgentEvent
	emitToolExecutionEnd(finalized, func(event AgentEvent) error {
		events = append(events, event)
		return nil
	})
	if len(events) != 1 || events[0].DurationMs == nil || *events[0].DurationMs != executed.durationMS {
		t.Fatalf("tool_execution_end events = %+v", events)
	}
}

// A tool that fails is timed too, and a call that never ran carries no duration.
func TestToolCallDurationOnFailureAndSkippedCalls(t *testing.T) {
	tool := &AgentTool{
		Name: "fails",
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
			time.Sleep(5 * time.Millisecond)
			return AgentToolResult{}, context.DeadlineExceeded
		},
	}
	prepared := &preparedToolCallValue{
		toolCall: ai.ToolCall{ID: "call-2", Name: "fails", Arguments: json.RawMessage(`{}`)},
		tool:     tool,
		args:     json.RawMessage(`{}`),
	}
	executed := executePreparedToolCall(prepared, context.Background(), func(AgentEvent) error { return nil })
	if !executed.isError {
		t.Fatal("a failing tool was not reported as an error")
	}
	if executed.durationMS < 5 {
		t.Fatalf("durationMS = %d, want the failing call to be timed as well", executed.durationMS)
	}

	// A call that never ran has no duration, and the field is omitted.
	skipped := finalizedToolCallOutcome{
		toolCall: ai.ToolCall{ID: "call-3", Name: "fails"}, isError: true,
	}
	if message := createToolResultMessage(skipped); message.DurationMs != nil {
		t.Fatalf("a skipped call carried a duration: %d", *message.DurationMs)
	}
}
