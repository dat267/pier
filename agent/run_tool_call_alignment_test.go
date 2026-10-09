package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream agent-loop.ts exports runToolCall with the same validation and hook pipeline.
func TestRunToolCallPreservesReturnedErrorFlag(t *testing.T) {
	toolCall := ai.ToolCall{ID: "tc", Name: "error", Arguments: json.RawMessage(`{}`)}
	assistant := createAssistantMessage(ai.ContentList{toolCall}, ai.StopToolUse)
	tool := AgentTool{
		Name: "error", Parameters: json.RawMessage(`{"type":"object"}`),
		Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
			return AgentToolResult{IsError: true, Content: []ai.Content{ai.TextContent{Text: "reported error"}}}, nil
		},
	}
	outcome := RunToolCall(toolCall, RunToolCallOptions{
		Tools: []AgentTool{tool}, AssistantMessage: assistant,
	})
	if !outcome.IsError {
		t.Fatalf("tool result IsError = false, want true: %+v", outcome)
	}
}

// Upstream agent-loop.ts exports runToolCall with the same validation and hook pipeline.
func TestRunToolCallUsesHooksAndStreamsUpdates(t *testing.T) {
	var beforeCalled, afterCalled bool
	var updateText string
	toolCall := ai.ToolCall{ID: "tc", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}
	assistant := createAssistantMessage(ai.ContentList{toolCall}, ai.StopToolUse)
	tool := AgentTool{
		Name: "echo", Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		Execute: func(_ string, args json.RawMessage, _ context.Context, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
			if string(args) != `{"text":"hello"}` {
				t.Fatalf("validated args = %s", args)
			}
			onUpdate(AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "working"}}})
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "done"}}, Details: json.RawMessage(`{"original":true}`)}, nil
		},
	}
	outcome := RunToolCall(toolCall, RunToolCallOptions{
		Tools: []AgentTool{tool}, AssistantMessage: assistant,
		Context: AgentContext{Messages: []ai.Message{createUserMessage("prompt")}},
		Signal:  context.Background(),
		BeforeToolCall: func(call *BeforeToolCallContext, _ context.Context) (*BeforeToolCallResult, error) {
			beforeCalled = call.ToolCall.ID == "tc" && string(call.Args) == `{"text":"hello"}`
			return nil, nil
		},
		AfterToolCall: func(call *AfterToolCallContext, _ context.Context) (*AfterToolCallResult, error) {
			afterCalled = call.ToolCall.ID == "tc" && !call.IsError
			return &AfterToolCallResult{Content: []ai.Content{ai.TextContent{Text: "overridden"}}, HasContent: true}, nil
		},
		OnUpdate: func(result AgentToolResult) {
			updateText = result.Content[0].(ai.TextContent).Text
		},
	})
	if !beforeCalled || !afterCalled || updateText != "working" {
		t.Fatalf("hooks/update = before:%v after:%v update:%q", beforeCalled, afterCalled, updateText)
	}
	if outcome.ToolCall.ID != "tc" || outcome.IsError || outcome.Result.Content[0].(ai.TextContent).Text != "overridden" {
		t.Fatalf("outcome = %+v", outcome)
	}
}
