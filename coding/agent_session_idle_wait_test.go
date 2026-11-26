package coding

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// A caller that waits for the session to become idle while a run is active is
// not released before that run settles, even when the previous run's idle
// release and this run's start overlap. Upstream _resolveIdleWaitIfIdle
// (core/agent-session.ts, pi v1.5.2 b4afdddf) checks and resolves in one
// synchronous step; the port must check idleness under the lock waiters
// register under, or a release decided before the new run started closes the
// channel the new waiter just joined. RPC abort waits this way.
func TestIdleWaitRegisteredDuringARunWaitsForItToSettle(t *testing.T) {
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	streamFn := func(m *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			time.Sleep(time.Duration(rand.IntN(300)) * time.Microsecond)
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "ok"}},
			}})
		}()
		return stream
	}
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, ConvertToLlm: ConvertToLlm})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	var settled atomic.Int64
	unsubscribe := session.Subscribe(func(event *SessionEvent) {
		if event.Type == SessionAgentSettled {
			settled.Add(1)
		}
	})
	defer unsubscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Two callers prompt, so one run can start while the other's previous run
	// is releasing its idle waiters.
	prompts := make(chan struct{})
	finished := make(chan struct{}, 2)
	for caller := range 2 {
		go func() {
			defer func() { finished <- struct{}{} }()
			// A prompt refused because the other caller's run is active
			// returns at once, so loop on settled runs, not attempts.
			for index := 0; settled.Load() < 300 && ctx.Err() == nil; index++ {
				_ = session.Prompt(ctx, fmt.Sprintf("prompt %d-%d", caller, index), nil)
			}
		}()
	}
	go func() { <-finished; <-finished; close(prompts) }()
	// Several waiters, so one can register while the other's release is in
	// progress.
	var checked atomic.Int64
	var early atomic.Int64
	const waiterCount = 4
	waiters := make(chan struct{}, waiterCount)
	for range waiterCount {
		go func() {
			defer func() { waiters <- struct{}{} }()
			for {
				select {
				case <-prompts:
					return
				default:
				}
				// Stagger the waiters, so one is free to register while the
				// other is still being released.
				time.Sleep(time.Duration(rand.IntN(400)) * time.Microsecond)
				before := settled.Load()
				if !session.IsStreaming() {
					continue
				}
				if err := session.waitForSessionIdle(ctx); err != nil {
					t.Error(err)
					return
				}
				// Decide the violation under the SAME lock the release takes
				// (resolveIdleWaitIfIdle holds promptState.mu while it checks
				// idleness). A bare IsStreaming() call after the wait cannot tell a
				// premature release from a NEW run having started in the meantime,
				// because those are two separate reads; holding the lock makes the
				// observation atomic with the release decision.
				state := session.prompt()
				state.mu.Lock()
				streamingOnReturn := state.runActive || session.Agent.State().IsStreaming
				settledOnReturn := settled.Load()
				state.mu.Unlock()
				checked.Add(1)
				// A premature release is "the wait returned while the run it was
				// waiting for is still streaming". Sampling IsStreaming after the
				// wait cannot separate that from a NEW run having started in the
				// meantime, so require both that no run settled during the wait
				// AND that the session is streaming now: if a run settled, the wait
				// was legitimately released by it.
				if settledOnReturn == before && streamingOnReturn {
					early.Add(1)
				}
			}
		}()
	}
	for range waiterCount {
		<-waiters
	}
	if checked.Load() == 0 {
		t.Fatal("no wait was registered during a run; the test checked nothing")
	}
	if early.Load() > 0 {
		t.Fatalf("%d of %d idle waits returned while the run was still streaming", early.Load(), checked.Load())
	}
}
