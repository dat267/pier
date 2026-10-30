package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// A tool's prepareArguments or a beforeToolCall hook that throws is a tool
// error in upstream (agent-loop.ts prepareToolCall try/catch); the port let the
// panic escape the loop and fail the whole run.
func TestPrepareArgumentsPanicBecomesToolError(t *testing.T) {
	streamFn, _ := mockStreamFn(
		createAssistantMessage(ai.ContentList{
			ai.ToolCall{ID: "t1", Name: "boom", Arguments: json.RawMessage(`{}`)},
		}, ai.StopToolUse),
		createAssistantMessage(ai.ContentList{ai.TextContent{Text: "after"}}, ai.StopStop),
	)
	tool := AgentTool{
		Name: "boom", Parameters: json.RawMessage(`{"type":"object"}`),
		PrepareArguments: func(args json.RawMessage) json.RawMessage {
			panic("prepare exploded")
		},
		Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
			return AgentToolResult{}, nil
		},
	}
	var toolResult *ai.ToolResultMessage
	var reachedAgentEnd bool
	messages := RunAgentLoop([]ai.Message{createUserMessage("hi")},
		AgentContext{Tools: []AgentTool{tool}},
		&AgentLoopConfig{Model: createModel(), ConvertToLlm: identityConverter},
		context.Background(),
		func(event AgentEvent) error {
			if result, ok := event.Message.(*ai.ToolResultMessage); ok && event.Type == MessageEnd {
				toolResult = result
			}
			if event.Type == AgentEnd {
				reachedAgentEnd = true
			}
			return nil
		}, streamFn)

	if !reachedAgentEnd {
		t.Fatal("the run did not reach agent_end")
	}
	if toolResult == nil {
		t.Fatalf("no tool result recorded: %v", messageRoles(messages))
	}
	if !toolResult.IsError {
		t.Error("the panicking tool call should record an error result")
	}
	if text := toolResult.Content[0].(ai.TextContent).Text; !strings.Contains(text, "prepare exploded") {
		t.Fatalf("tool error = %q, want the panic message", text)
	}
	if last := messages[len(messages)-1]; ai.RoleOf(last) != ai.RoleAssistant {
		t.Fatalf("messages = %v; the loop should continue after the tool error", messageRoles(messages))
	}
}

// A tool that throws (panics) is a tool error result in upstream
// (agent-loop.ts executePreparedToolCall try/catch); the error return path is
// covered; this pins the panic path.
func TestExecutePanicBecomesToolError(t *testing.T) {
	streamFn, _ := mockStreamFn(
		createAssistantMessage(ai.ContentList{
			ai.ToolCall{ID: "t1", Name: "boom", Arguments: json.RawMessage(`{}`)},
		}, ai.StopToolUse),
		createAssistantMessage(ai.ContentList{ai.TextContent{Text: "after"}}, ai.StopStop),
	)
	tool := AgentTool{
		Name: "boom", Parameters: json.RawMessage(`{"type":"object"}`),
		Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
			panic("execute exploded")
		},
	}
	var toolResult *ai.ToolResultMessage
	messages := RunAgentLoop([]ai.Message{createUserMessage("hi")},
		AgentContext{Tools: []AgentTool{tool}},
		&AgentLoopConfig{Model: createModel(), ConvertToLlm: identityConverter},
		context.Background(),
		func(event AgentEvent) error {
			if result, ok := event.Message.(*ai.ToolResultMessage); ok && event.Type == MessageEnd {
				toolResult = result
			}
			return nil
		}, streamFn)

	if toolResult == nil || !toolResult.IsError {
		t.Fatalf("no error tool result recorded: %v", messageRoles(messages))
	}
	if text := toolResult.Content[0].(ai.TextContent).Text; !strings.Contains(text, "execute exploded") {
		t.Fatalf("tool error = %q, want the panic message", text)
	}
	if last := messages[len(messages)-1]; ai.RoleOf(last) != ai.RoleAssistant {
		t.Fatalf("messages = %v; the loop should continue after the tool error", messageRoles(messages))
	}
}

// An afterToolCall hook that panics is a tool error result in upstream
// (agent-loop.ts finalizeExecutedToolCall try/catch), not a run failure.
func TestAfterToolCallPanicBecomesToolError(t *testing.T) {
	streamFn, _ := mockStreamFn(
		createAssistantMessage(ai.ContentList{
			ai.ToolCall{ID: "t1", Name: "ok", Arguments: json.RawMessage(`{}`)},
		}, ai.StopToolUse),
		createAssistantMessage(ai.ContentList{ai.TextContent{Text: "after"}}, ai.StopStop),
	)
	tool := AgentTool{
		Name: "ok", Parameters: json.RawMessage(`{"type":"object"}`),
		Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "fine"}}}, nil
		},
	}
	config := &AgentLoopConfig{
		Model: createModel(), ConvertToLlm: identityConverter,
		AfterToolCall: func(*AfterToolCallContext, context.Context) (*AfterToolCallResult, error) {
			panic("after exploded")
		},
	}
	var toolResult *ai.ToolResultMessage
	messages := RunAgentLoop([]ai.Message{createUserMessage("hi")},
		AgentContext{Tools: []AgentTool{tool}}, config, context.Background(),
		func(event AgentEvent) error {
			if result, ok := event.Message.(*ai.ToolResultMessage); ok && event.Type == MessageEnd {
				toolResult = result
			}
			return nil
		}, streamFn)

	if toolResult == nil || !toolResult.IsError {
		t.Fatalf("no error tool result recorded: %v", messageRoles(messages))
	}
	if text := toolResult.Content[0].(ai.TextContent).Text; !strings.Contains(text, "after exploded") {
		t.Fatalf("tool error = %q, want the panic message", text)
	}
	if last := messages[len(messages)-1]; ai.RoleOf(last) != ai.RoleAssistant {
		t.Fatalf("messages = %v; the loop should continue after the hook error", messageRoles(messages))
	}
}

// A worker panic in the parallel executor must fail the run through the loop's
// panic contract, not kill the process: the sequential executor's emits
// propagate on the loop goroutine, so the parallel path has to re-panic there.
func TestParallelWorkerPanicReachesTheCaller(t *testing.T) {
	streamFn, _ := mockStreamFn(createAssistantMessage(ai.ContentList{
		ai.ToolCall{ID: "t1", Name: "ok", Arguments: json.RawMessage(`{}`)},
		ai.ToolCall{ID: "t2", Name: "ok", Arguments: json.RawMessage(`{}`)},
	}, ai.StopToolUse))
	tool := AgentTool{
		Name: "ok", Parameters: json.RawMessage(`{"type":"object"}`),
		Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
			return AgentToolResult{}, nil
		},
	}
	config := &AgentLoopConfig{Model: createModel(), ConvertToLlm: identityConverter}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		RunAgentLoop([]ai.Message{createUserMessage("hi")},
			AgentContext{Tools: []AgentTool{tool}}, config, context.Background(),
			func(event AgentEvent) error {
				if event.Type == ToolExecutionEnd {
					panic("end emit exploded")
				}
				return nil
			}, streamFn)
	}()
	if recovered == nil {
		t.Fatal("a parallel worker panic did not reach the caller")
	}
}

// D311: Go serializes updates within one tool invocation to preserve the
// synchronous single-writer event-sink contract. Upstream agent-loop.ts starts
// each onUpdate promise in call order and awaits Promise.all, so asynchronous
// listeners may overlap even though their start order is preserved.
func TestToolUpdatesAreSerializedInCallOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	active := 0
	overlap := false
	// The sleep makes an in-flight delivery visible to the others: a concurrent
	// implementation overlaps every time, a serialized one never does.
	sink := func(result AgentToolResult) {
		mu.Lock()
		active++
		if active > 1 {
			overlap = true
		}
		order = append(order, result.Content[0].(ai.TextContent).Text)
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
	}
	tool := AgentTool{
		Name: "updates", Parameters: json.RawMessage(`{"type":"object"}`),
		Execute: func(_ string, _ json.RawMessage, _ context.Context, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
			for _, text := range []string{"one", "two", "three", "four", "five"} {
				onUpdate(AgentToolResult{Content: []ai.Content{ai.TextContent{Text: text}}})
			}
			return AgentToolResult{}, nil
		},
	}
	outcome := RunToolCall(ai.ToolCall{ID: "tc", Name: "updates", Arguments: json.RawMessage(`{}`)}, RunToolCallOptions{
		Tools: []AgentTool{tool}, AssistantMessage: createAssistantMessage(nil, ai.StopStop),
		Context: AgentContext{}, Signal: context.Background(), OnUpdate: sink,
	})
	if outcome.IsError {
		t.Fatalf("outcome = %+v", outcome)
	}
	mu.Lock()
	defer mu.Unlock()
	if overlap {
		t.Fatalf("tool updates were delivered concurrently: %v", order)
	}
	want := []string{"one", "two", "three", "four", "five"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("update order = %v; want %v", order, want)
	}
}
