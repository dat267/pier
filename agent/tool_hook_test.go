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
		Tools: []AgentTool{{Name: "probe", Parameters: json.RawMessage(`{"type":"object"}`), Execute: func(context.Context, string, json.RawMessage, func(AgentToolResult)) (AgentToolResult, error) {
			t.Fatal("tool executed after failed hook")
			return AgentToolResult{}, nil
		}}},
		BeforeToolCall: func(context.Context, *BeforeToolCallContext) (*BeforeToolCallResult, error) {
			cancel()
			return nil, errors.New("fixture hook failed")
		},
	})
	if !outcome.IsError || len(outcome.Result.Content) != 1 || outcome.Result.Content[0].(ai.TextContent).Text != "fixture hook failed" {
		t.Fatalf("outcome = %+v, want the hook failure instead of Operation aborted", outcome)
	}
}

// pi v1.4.0 agent-loop.ts merges content with ??, not a presence-only check.
// A null/undefined override keeps content; a present empty array replaces it.
func TestAfterToolCallNullContentKeepsExecutedContent(t *testing.T) {
	for _, content := range [][]ai.Content{nil, {}} {
		call := ai.ToolCall{ID: "call", Name: "probe", Arguments: json.RawMessage(`{}`)}
		outcome := RunToolCall(call, RunToolCallOptions{
			Tools: []AgentTool{{Name: "probe", Parameters: json.RawMessage(`{"type":"object"}`), Execute: func(context.Context, string, json.RawMessage, func(AgentToolResult)) (AgentToolResult, error) {
				return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "original"}}}, nil
			}}},
			AfterToolCall: func(context.Context, *AfterToolCallContext) (*AfterToolCallResult, error) {
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

// pi v1.4.0 agent-loop.ts finalizeExecutedToolCall merges details with ??,
// so a null/undefined after-hook value keeps the executed result's details.
func TestAfterToolCallNullDetailsKeepExecutedDetails(t *testing.T) {
	for _, details := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage(" \nnull\t")} {
		call := ai.ToolCall{ID: "call", Name: "probe", Arguments: json.RawMessage(`{}`)}
		outcome := RunToolCall(call, RunToolCallOptions{
			Tools: []AgentTool{{Name: "probe", Parameters: json.RawMessage(`{"type":"object"}`), Execute: func(context.Context, string, json.RawMessage, func(AgentToolResult)) (AgentToolResult, error) {
				return AgentToolResult{Details: json.RawMessage(`{"original":true}`)}, nil
			}}},
			AfterToolCall: func(context.Context, *AfterToolCallContext) (*AfterToolCallResult, error) {
				return &AfterToolCallResult{HasDetails: true, Details: details}, nil
			},
		})
		if outcome.IsError || string(outcome.Result.Details) != `{"original":true}` {
			t.Errorf("after details=%q: outcome=%+v, want original details", details, outcome)
		}
	}
}
