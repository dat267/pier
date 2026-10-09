package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream agent-loop.ts awaits onUpdate promises; rejected update listeners fail the tool call.
func TestToolUpdateListenerFailureBecomesToolError(t *testing.T) {
	wantErr := errors.New("update listener failed")
	tool := &AgentTool{
		Name: "updates",
		Execute: func(_ string, _ json.RawMessage, _ context.Context, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
			onUpdate(AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "partial"}}})
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "success"}}}, nil
		},
	}
	prepared := &preparedToolCallValue{
		toolCall: ai.ToolCall{ID: "tc", Name: "updates"}, tool: tool,
	}
	executed := executePreparedToolCall(prepared, context.Background(), func(AgentEvent) error {
		return wantErr
	})
	if !executed.isError {
		t.Fatal("update listener failure did not fail tool call")
	}
	if got := executed.result.Content[0].(ai.TextContent).Text; got != wantErr.Error() {
		t.Fatalf("tool error = %q, want %q", got, wantErr)
	}
}
