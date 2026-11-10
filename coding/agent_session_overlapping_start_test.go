package coding

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// A run start that loses a race with another (both passed the "not
// streaming" check: a goal round off the loop and a user prompt on it) fails
// with "already processing" and leaves the winning run's state alone, so the
// session still reports it as streaming and an abort still reaches it.
// Upstream _startRun (agent-session.ts, v1.5.0 f9a26365) has the same window
// at its awaits and lets the loser's finally settle the session.
func TestALosingRunStartLeavesTheActiveRunsState(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	started := make(chan struct{}, 1)
	session := newPromptSession(t, model, func(m *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		started <- struct{}{}
		go func() {
			<-options.Ctx.Done()
			aborted := &ai.AssistantMessage{API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopAborted}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: aborted})
		}()
		return stream
	})
	first := make(chan error, 1)
	go func() { first <- session.Prompt(context.Background(), "first", nil) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first run did not start")
	}

	// The loser got past its check before the winner was marked active.
	err := session.runAgentPrompt(context.Background(), []ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}}})
	if err == nil || !strings.Contains(err.Error(), "already processing") {
		t.Fatalf("losing start err = %v, want already processing", err)
	}
	if !session.IsStreaming() {
		t.Fatal("the losing start marked the session idle while the first run streams")
	}
	idle := make(chan struct{})
	go func() {
		_ = session.waitForSessionIdle(context.Background())
		close(idle)
	}()
	select {
	case <-idle:
		t.Fatal("idle waiters were released while the first run streams")
	case <-time.After(50 * time.Millisecond):
	}

	session.AbortAsync()
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("abort did not reach the first run")
	}
	select {
	case <-idle:
	case <-time.After(5 * time.Second):
		t.Fatal("the session did not become idle after the first run ended")
	}
}
