package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// TestRunAgentLoopFailsWhenTheStreamEndsWithoutATerminalEvent pins pi v1.5.2
// (b4afdddf) packages/agent/src/agent-loop.ts streamAssistantResponse: after
// `for await (const event of response)` the loop awaits `response.result()`,
// which resolves undefined for a stream that ended with no done/error event, and
// `Object.assign(undefined, {...})` throws, so the run fails. The port awaited
// its result on a context that ignores cancellation, so a custom stream function
// that ends its stream empty blocked the loop forever instead of failing it.
func TestRunAgentLoopFailsWhenTheStreamEndsWithoutATerminalEvent(t *testing.T) {
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		stream.End(nil)
		return stream
	}
	config := &AgentLoopConfig{Model: createModel(), ConvertToLlm: identityConverter}

	type outcome struct {
		messages []ai.Message
		panicked any
	}
	done := make(chan outcome, 1)
	go func() {
		var result outcome
		func() {
			defer func() { result.panicked = recover() }()
			result.messages = RunAgentLoop(
				[]ai.Message{createUserMessage("hello")},
				AgentContext{},
				config,
				context.Background(),
				func(AgentEvent) error { return nil },
				streamFn,
			)
		}()
		done <- result
	}()

	select {
	case result := <-done:
		if result.panicked == nil {
			t.Fatalf("a stream without a terminal event returned %d messages instead of failing", len(result.messages))
		}
		if message, ok := result.panicked.(error); !ok || !strings.Contains(message.Error(), "terminal") {
			t.Fatalf("failure = %v, want an error naming the missing terminal event", result.panicked)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the loop blocked on a stream that ended without a terminal event")
	}
}
