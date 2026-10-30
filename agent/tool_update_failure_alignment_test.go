package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 (4a080252), packages/agent/src/agent-loop.ts awaits
// onUpdate promises; a rejected event sink rejects the tool call and the agent
// wrapper handles it as a run failure.
func TestToolUpdateListenerFailureRejectsPreparedCall(t *testing.T) {
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
	var got error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				got, _ = recovered.(error)
			}
		}()
		executePreparedToolCall(prepared, context.Background(), func(AgentEvent) error {
			return wantErr
		})
	}()
	if !errors.Is(got, wantErr) {
		t.Fatalf("tool call rejection = %v, want %v", got, wantErr)
	}
}

func TestAgentToolUpdateListenerFailureBecomesRunFailure(t *testing.T) {
	wantErr := errors.New("update listener failed")
	tool := AgentTool{
		Name: "updates", Description: "", Parameters: json.RawMessage(`{"type":"object"}`), Label: "updates",
		Execute: func(_ string, _ json.RawMessage, _ context.Context, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
			onUpdate(AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "partial"}}})
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "success"}}}, nil
		},
	}
	assistant := createAssistantMessage(ai.ContentList{ai.ToolCall{ID: "tc", Name: "updates", Arguments: json.RawMessage(`{}`)}}, ai.StopToolUse)
	followUp := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "should not run"}}, ai.StopStop)
	streamFn, calls := mockStreamFn(assistant, followUp)
	agent, err := NewAgent(&AgentOptions{
		StreamFn: streamFn,
		InitialState: &AgentInitialState{
			Model: createModel(), Tools: []AgentTool{tool},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	agent.Subscribe(func(event AgentEvent, _ context.Context) error {
		if event.Type == ToolExecutionUpdate {
			return wantErr
		}
		return nil
	})
	if err := agent.PromptText(context.Background(), "run"); err != nil {
		t.Fatal(err)
	}
	state := agent.State()
	if state.ErrorMessage != wantErr.Error() {
		t.Fatalf("run error = %q, want %q", state.ErrorMessage, wantErr)
	}
	if *calls != 1 {
		t.Fatalf("provider calls = %d, want no continuation after update failure", *calls)
	}
	last, ok := state.Messages[len(state.Messages)-1].(*ai.AssistantMessage)
	if !ok || last.StopReason != ai.StopError || last.ErrorMessage == nil || *last.ErrorMessage != wantErr.Error() {
		t.Fatalf("failure message = %#v", state.Messages[len(state.Messages)-1])
	}
	for _, message := range state.Messages {
		if _, ok := message.(*ai.ToolResultMessage); ok {
			t.Fatal("failed update listener must not produce a tool result")
		}
	}
}
