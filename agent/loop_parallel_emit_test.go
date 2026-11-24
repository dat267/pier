package agent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// TestParallelToolEndsDoNotOverlapInTheSink pins pi v1.5.2 (b4afdddf)
// packages/agent/src/agent-loop.ts executeToolCallsParallel: the reference
// awaits every emit, so its sink is never invoked while another invocation runs.
// The port emitted each finishing call's tool_execution_end from that call's
// goroutine, so two parallel tools called the caller's listener at the same
// time; `go test -race ./agent` caught it through
// TestRunAgentLoopEventsMatchReference/parallel-batch.
func TestParallelToolEndsDoNotOverlapInTheSink(t *testing.T) {
	tool := func(name string) AgentTool {
		return AgentTool{
			Name: name, Label: name, Description: name,
			Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
			Execute: func(context.Context, string, json.RawMessage, func(AgentToolResult)) (AgentToolResult, error) {
				time.Sleep(10 * time.Millisecond)
				return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: name}}}, nil
			},
		}
	}
	calls := 0
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		calls++
		if calls == 1 {
			message := createAssistantMessage(ai.ContentList{
				ai.ToolCall{ID: "1", Name: "a", Arguments: json.RawMessage(`{}`)},
				ai.ToolCall{ID: "2", Name: "b", Arguments: json.RawMessage(`{}`)},
				ai.ToolCall{ID: "3", Name: "c", Arguments: json.RawMessage(`{}`)},
			}, ai.StopToolUse)
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopToolUse, Message: message})
			return stream
		}
		message := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "done"}}, ai.StopStop)
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		return stream
	}

	var inside atomic.Int32
	var overlapped atomic.Bool
	var mu sync.Mutex
	ends := 0
	emit := func(event AgentEvent) error {
		if inside.Add(1) > 1 {
			overlapped.Store(true)
		}
		// Widen the window: an unserialized emission from another finishing tool
		// enters here while this one is still inside.
		time.Sleep(2 * time.Millisecond)
		inside.Add(-1)
		if event.Type == ToolExecutionEnd {
			mu.Lock()
			ends++
			mu.Unlock()
		}
		return nil
	}
	RunAgentLoop(
		context.Background(),
		[]ai.Message{createUserMessage("run")},
		AgentContext{Tools: []AgentTool{tool("a"), tool("b"), tool("c")}},
		&AgentLoopConfig{Model: createModel(), ConvertToLlm: defaultConvertToLlm},
		emit,
		streamFn,
	)
	if overlapped.Load() {
		t.Fatal("the sink ran for two tool events at once")
	}
	mu.Lock()
	defer mu.Unlock()
	if ends != 3 {
		t.Fatalf("tool_execution_end count = %d, want 3", ends)
	}
}
