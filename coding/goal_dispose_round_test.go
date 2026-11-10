package coding

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// D223's native goal scheduler must not launch deferred work after its session
// is disposed. Reference: pi v1.5.0 (f9a26365), coding-agent/src/core/
// agent-session.ts dispose and agent-session-runtime.ts teardownCurrent.
func TestDisposedSessionDoesNotStartAReservedGoalRound(t *testing.T) {
	manager, dir := newTestSession(t)
	calls := 0
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Goal: true, Control: &AgentSessionControl{},
		Model: &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128},
		StreamFn: func(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			calls++
			stream := ai.NewAssistantMessageEventStream()
			message := &ai.AssistantMessage{API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopError, ErrorMessage: new("bounded test response")}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopError, Message: message})
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var pending []func(context.Context) error
	session.Goal.SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
	session.Goal.HandleCommand("set queued before teardown")
	if len(pending) != 1 {
		t.Fatalf("reserved rounds = %d, want 1", len(pending))
	}
	session.Dispose()
	_ = pending[0](context.Background())
	if calls != 0 {
		t.Fatalf("model calls after disposal = %d, want 0", calls)
	}
}

// A reserved goal round racing Dispose either starts nothing or starts a run
// Dispose's abort reaches: the disposed check and the run start share one
// critical section (D223, D385).
func TestReservedGoalRoundRacingDisposeLeavesNoUnabortableRun(t *testing.T) {
	const rounds = 20
	for round := range rounds {
		manager, dir := newTestSession(t)
		var unaborted atomic.Int32
		var calls sync.WaitGroup
		session, err := NewAgentSession(&SessionConfig{
			Cwd: dir, Sessions: manager, Goal: true, Control: &AgentSessionControl{},
			Model: &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128},
			StreamFn: func(model *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				stream := ai.NewAssistantMessageEventStream()
				calls.Add(1)
				go func() {
					defer calls.Done()
					message := &ai.AssistantMessage{API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopAborted}
					select {
					case <-options.Ctx.Done():
					case <-time.After(2 * time.Second):
						unaborted.Add(1)
					}
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: message})
				}()
				return stream
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var pending []func(context.Context) error
		session.Goal.SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
		session.Goal.HandleCommand("set raced by teardown")
		if len(pending) != 1 {
			t.Fatalf("reserved rounds = %d, want 1", len(pending))
		}
		start := make(chan struct{})
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			<-start
			_ = pending[0](context.Background())
		}()
		go func() {
			defer group.Done()
			<-start
			// The round does more work before its run starts than Dispose
			// does before marking the session; stagger Dispose so some rounds
			// land it while the round is between its checks and the start.
			time.Sleep(time.Duration(round) * 25 * time.Microsecond)
			session.Dispose()
		}()
		close(start)
		group.Wait()
		calls.Wait()
		if unaborted.Load() != 0 {
			t.Fatal("the goal round's model call outlived Dispose uncancelled")
		}
	}
}
