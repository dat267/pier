package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dat267/pier/ai"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type observingRunParent struct {
	context.Context
	done                chan struct{}
	registered, stopped atomic.Int32
}

func (p *observingRunParent) Done() <-chan struct{} { return p.done }
func (p *observingRunParent) AfterFunc(func()) func() bool {
	p.registered.Add(1)
	return func() bool { p.stopped.Add(1); return true }
}

// Reference: pi v1.4.0 agent.ts finishRun releases runtime-owned run state.
// Go additionally owns the registration on a caller's cancellation context.
func TestFinishedRunReleasesParentCancellation(t *testing.T) {
	p := &observingRunParent{Context: context.Background(), done: make(chan struct{})}
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := a.PromptText(p, "hello"); err != nil {
			t.Fatal(err)
		}
	}
	if p.registered.Load() != 3 || p.stopped.Load() != 3 {
		t.Fatalf("registered=%d stopped=%d", p.registered.Load(), p.stopped.Load())
	}
	if a.State().IsStreaming {
		t.Fatal("run is still active")
	}
}

// A481: runWithLifecycle used to publish activeRun = nil and unlock before it
// called runContexts.Delete(a) on a process-wide map. A run that started in
// that window stored its own context, and the older run's Delete then removed
// it, so a live run observed a nil context. The context now lives on activeRun
// itself. Reference: upstream has no equivalent map; the invariant is
// local, "a live run always has its context".
func TestRunContextIsPresentForTheWholeRun(t *testing.T) {
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	var lost atomic.Int64
	exec := func(context.Context) error {
		if a.runContext() == nil {
			lost.Add(1)
		}
		return nil
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40000; i++ {
				_ = a.runWithLifecycle(context.Background(), exec)
			}
		}()
	}
	wg.Wait()
	if n := lost.Load(); n != 0 {
		t.Fatalf("runContext() was nil for a live run %d times", n)
	}
}

// Port of the core subset of packages/agent/test/agent.test.ts.

func unusedStreamFn() StreamFn {
	return func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			msg := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "unused"}}, ai.StopStop)
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: msg})
		}()
		return stream
	}
}

func makeTool(name string) AgentTool {
	return AgentTool{
		Name: name, Description: name + " tool", Parameters: json.RawMessage(`{"type":"object"}`),
		Label: name,
		Execute: func(string, json.RawMessage, context.Context, func(AgentToolResult)) (AgentToolResult, error) {
			return AgentToolResult{Content: []ai.Content{ai.TextContent{Text: name}}, Details: json.RawMessage(`{}`)}, nil
		},
	}
}

func TestAgentRunFailureEmitsLifecycle(t *testing.T) {
	agent, _ := NewAgent(&AgentOptions{
		StreamFn: func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			panic("provider exploded")
		},
	})
	var events []AgentEvent
	agent.Subscribe(func(event AgentEvent, ctx context.Context) error {
		events = append(events, event)
		return nil
	})
	if err := agent.PromptText(context.Background(), "hello"); err != nil {
		t.Fatalf("run failure should be folded into events, not returned: %v", err)
	}
	var types []string
	for _, e := range events {
		types = append(types, e.Type)
	}
	// Upstream expectation: the loop's agent_start/turn_start precede the
	// synthesized failure lifecycle.
	want := []string{AgentStart, TurnStart, MessageStart, MessageEnd, MessageStart, MessageEnd, TurnEnd, AgentEnd}
	if len(types) != len(want) {
		t.Fatalf("events = %v; want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("events = %v; want %v", types, want)
		}
	}
	lastMessage := agent.State().Messages[len(agent.State().Messages)-1].(*ai.AssistantMessage)
	if lastMessage.StopReason != ai.StopError || *lastMessage.ErrorMessage != "provider exploded" {
		t.Fatalf("lastMessage = %+v", lastMessage)
	}
	if agent.State().ErrorMessage != "provider exploded" {
		t.Fatalf("state errorMessage = %q", agent.State().ErrorMessage)
	}
}

func TestAgentForwardsProviderStreamEvent(t *testing.T) {
	called := false
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		options.OnProviderStreamEvent(json.RawMessage(`{"type":"response.created"}`), model)
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop,
			Message: createAssistantMessage(ai.ContentList{ai.TextContent{Text: "ok"}}, ai.StopStop)})
		return stream
	}
	agent, err := NewAgent(&AgentOptions{
		StreamFn: streamFn,
		OnProviderStreamEvent: func(data json.RawMessage, model *ai.Model) {
			called = string(data) == `{"type":"response.created"}` && model != nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.PromptText(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("Agent did not forward OnProviderStreamEvent")
	}
}

func TestAgentForwardsSessionID(t *testing.T) {
	var captured *ai.SimpleStreamOptions
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		captured = options
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop,
			Message: createAssistantMessage(ai.ContentList{ai.TextContent{Text: "ok"}}, ai.StopStop)})
		return stream
	}
	agent, _ := NewAgent(&AgentOptions{StreamFn: streamFn, SessionID: "chat-42"})
	if err := agent.PromptText(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if captured == nil || captured.SessionID != "chat-42" {
		t.Fatalf("sessionId = %+v", captured)
	}
}

// The Agent stores the tool-call hooks in its options; they must reach the loop
// config, or every call is silently ungated (regression: they were dropped,
// which left BeforeToolCall inert on Agent).
func TestAgentBeforeToolCallReachesTheLoop(t *testing.T) {
	ran := 0
	tool := makeTool("echo")
	original := tool.Execute
	tool.Execute = func(id string, params json.RawMessage, ctx context.Context, onUpdate func(AgentToolResult)) (AgentToolResult, error) {
		ran++
		return original(id, params, ctx, onUpdate)
	}

	assistant := createAssistantMessage(ai.ContentList{
		ai.ToolCall{ID: "tc1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)},
	}, ai.StopToolUse)
	streamFn, _ := mockStreamFn(assistant)

	a, err := NewAgent(&AgentOptions{
		InitialState: &AgentInitialState{SystemPrompt: "sys", Tools: []AgentTool{tool}, Model: createModel()},
		StreamFn:     streamFn,
		BeforeToolCall: func(call *BeforeToolCallContext, ctx context.Context) (*BeforeToolCallResult, error) {
			if call.ToolCall.Name == "echo" {
				return &BeforeToolCallResult{Block: true, Reason: "denied by the test"}, nil
			}
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PromptText(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if ran != 0 {
		t.Fatalf("the tool ran %d times, want the hook to block it", ran)
	}

	var blocked *ai.ToolResultMessage
	for _, message := range a.State().Messages {
		if result, ok := message.(*ai.ToolResultMessage); ok {
			blocked = result
		}
	}
	if blocked == nil {
		t.Fatal("no tool result recorded for the blocked call")
	}
	if !blocked.IsError {
		t.Error("a blocked call should record an error result")
	}
	if !strings.Contains(blocked.Content[0].(ai.TextContent).Text, "denied by the test") {
		t.Errorf("result = %+v, want the hook's reason", blocked.Content)
	}
}

// A run failure from a canceled context is an abort; the failure message must
// carry the canonical abort text so the transcript shows "Operation aborted"
// instead of the transport's raw "context canceled" (upstream throws
// Error("Request was aborted") when the signal is aborted).
func TestHandleRunFailureMapsCanceledContextToAbortMessage(t *testing.T) {
	for _, runErr := range []error{
		context.Canceled,
		context.DeadlineExceeded,
		fmt.Errorf("provider read: %w", context.Canceled),
	} {
		agent, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
		if err != nil {
			t.Fatal(err)
		}
		agent.handleRunFailure(context.Background(), runErr, true)

		messages := agent.State().Messages
		if len(messages) == 0 {
			t.Fatalf("%v: no failure message emitted", runErr)
		}
		failure, ok := messages[len(messages)-1].(*ai.AssistantMessage)
		if !ok {
			t.Fatalf("%v: failure message = %T", runErr, messages[len(messages)-1])
		}
		if failure.StopReason != ai.StopAborted {
			t.Fatalf("%v: stopReason = %s, want aborted", runErr, failure.StopReason)
		}
		if failure.ErrorMessage == nil || *failure.ErrorMessage != ai.RequestAbortedMessage {
			got := "<nil>"
			if failure.ErrorMessage != nil {
				got = *failure.ErrorMessage
			}
			t.Fatalf("%v: errorMessage = %q, want %q", runErr, got, ai.RequestAbortedMessage)
		}
	}
}

// Concurrent appenders each land in the transcript, and a State snapshot has
// no spare capacity, so appending to it cannot write into slots the agent's
// own transcript will reuse. Upstream mutates agent.state.messages on one
// event loop (packages/agent/src/agent.ts, v1.5.0 f9a26365).
func TestAppendMessagesKeepsEveryConcurrentAppend(t *testing.T) {
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	const appenders = 32
	var group sync.WaitGroup
	start := make(chan struct{})
	for range appenders {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			a.AppendMessages(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "x"}})
		}()
	}
	close(start)
	group.Wait()
	messages := a.State().Messages
	if len(messages) != appenders {
		t.Fatalf("transcript has %d of %d appends", len(messages), appenders)
	}
	if cap(messages) != len(messages) {
		t.Fatalf("snapshot cap = %d, len = %d: appending to it would share the agent's array", cap(messages), len(messages))
	}
	_ = append(messages, &ai.UserMessage{Content: ai.StringOrBlocks{Text: "snapshot only"}})
	a.AppendMessages(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "agent"}})
	if last := a.State().Messages[appenders]; ai.RoleOf(last) != ai.RoleUser || last.(*ai.UserMessage).Content.Text != "agent" {
		t.Fatalf("the snapshot's append leaked into the transcript: %+v", last)
	}
}

// pi v1.4.0 (4a080252), core/sdk.ts cacheContextIsCurrent checks object
// identity for every warmed-prefix message, not a mutation epoch or count.
func TestMessagePrefixCurrencyMatchesUpstream(t *testing.T) {
	for _, fixture := range []struct {
		operation string
		want      bool
	}{
		{"unchanged", true}, {"append", true}, {"replace", false},
		{"restore", true}, {"truncate", false}, {"model-change", false},
		{"replace-unwarmed-tail", true},
	} {
		t.Run(fixture.operation, func(t *testing.T) {
			model := &ai.Model{ID: "model", Provider: "fixture"}
			original := []ai.Message{
				&ai.SystemMessage{Content: ai.StringOrBlocks{Text: "system"}},
				&ai.UserMessage{Content: ai.StringOrBlocks{Text: "original"}},
			}
			a, err := NewAgent(&AgentOptions{InitialState: &AgentInitialState{Model: model, Messages: original}, StreamFn: unusedStreamFn()})
			if err != nil {
				t.Fatal(err)
			}
			current := a.MessagePrefixIsCurrent(model)
			replacement := []ai.Message{original[0], &ai.UserMessage{Content: ai.StringOrBlocks{Text: "replaced"}}}
			switch fixture.operation {
			case "append":
				a.SetMessages(append(append([]ai.Message(nil), original...), &ai.AssistantMessage{}))
			case "replace":
				a.SetMessages(replacement)
			case "restore":
				a.SetMessages(replacement)
				a.SetMessages(original)
			case "truncate":
				a.SetMessages(original[:1])
			case "model-change":
				a.SetModel(&ai.Model{ID: "other", Provider: "fixture"})
			case "replace-unwarmed-tail":
				a.SetMessages(append(append([]ai.Message(nil), original...), &ai.AssistantMessage{}))
				a.SetMessages(append(append([]ai.Message(nil), original...), &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "changed tail"}}}))
			}
			if got := current(); got != fixture.want {
				t.Fatalf("currency after %s = %v, want %v", fixture.operation, got, fixture.want)
			}
		})
	}
}

// TestDefaultStreamFn covers stream-fn.ts: the fallback is unset by default,
// round-trips a configured function, and clears back to unset.
func TestDefaultStreamFn(t *testing.T) {
	previous, hadPrevious := defaultStreamFn, defaultStreamFn != nil
	t.Cleanup(func() {
		if hadPrevious {
			SetDefaultStreamFn(previous)
		} else {
			SetDefaultStreamFn(nil)
		}
	})
	SetDefaultStreamFn(nil)
	if _, err := GetDefaultStreamFn(); err == nil || !strings.Contains(err.Error(), "No default stream function configured") {
		t.Fatalf("unset err = %v", err)
	}

	called := false
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		called = true
		return ai.NewAssistantMessageEventStream()
	}
	SetDefaultStreamFn(streamFn)
	got, err := GetDefaultStreamFn()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("default stream function is nil after being set")
	}
	_ = got(nil, ai.TranscriptContext{}, nil)
	if !called {
		t.Fatal("the configured stream function was not returned")
	}

	SetDefaultStreamFn(nil)
	if _, err := GetDefaultStreamFn(); err == nil {
		t.Fatal("clearing the default must leave it unset")
	}
}

// Upstream agent-loop.ts:409 wraps the stream result as
// Object.assign(result, { thinkingLevel: config.reasoning ?? "off" }), so every
// loop-produced assistant message records the requested Pi thinking level.
func TestAssistantMessageRecordsRequestedThinkingLevel(t *testing.T) {
	for _, level := range []ai.ThinkingLevel{"", ai.ThinkHigh} {
		streamFn, _ := mockStreamFn(createAssistantMessage(ai.ContentList{ai.TextContent{Text: "ok"}}, ai.StopStop))
		messages := RunAgentLoop(
			[]ai.Message{createUserMessage("hi")},
			AgentContext{},
			&AgentLoopConfig{
				Model:        createModel(),
				ConvertToLlm: identityConverter,
				Reasoning:    level,
			},
			context.Background(),
			func(AgentEvent) error { return nil },
			streamFn,
		)
		final, ok := messages[len(messages)-1].(*ai.AssistantMessage)
		if !ok {
			t.Fatalf("reasoning %q: last message = %T, want assistant", level, messages[len(messages)-1])
		}
		want := level
		if want == "" {
			want = ai.ThinkOff
		}
		if final.ThinkingLevel == nil || *final.ThinkingLevel != want {
			t.Fatalf("reasoning %q: thinkingLevel = %v, want %q", level, final.ThinkingLevel, want)
		}
		encoded, err := ai.MarshalMessage(final)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"thinkingLevel":"`+want+`"`) {
			t.Fatalf("reasoning %q: persisted message lacks thinkingLevel: %s", level, encoded)
		}
	}
}

// Upstream agent-loop.ts rounds performance.now() deltas with Math.round.
func TestDurationMillisecondsRoundsToNearestMillisecond(t *testing.T) {
	for _, test := range []struct {
		elapsed time.Duration
		want    int64
	}{
		{elapsed: 1499 * time.Microsecond, want: 1},
		{elapsed: 1500 * time.Microsecond, want: 2},
		{elapsed: 2499 * time.Microsecond, want: 2},
		{elapsed: 2500 * time.Microsecond, want: 3},
	} {
		if got := roundDurationMilliseconds(test.elapsed); got != test.want {
			t.Errorf("roundDurationMilliseconds(%s) = %d, want %d", test.elapsed, got, test.want)
		}
	}
}

// Reference: pi v1.4.0 agent.ts runWithLifecycle awaits handleRunFailure;
// an error from failure-event listeners rejects the prompt.
func TestFailureLifecycleListenerErrorReachesCaller(t *testing.T) {
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("listener failed")
	a.Subscribe(func(AgentEvent, context.Context) error { return want })
	if err := a.PromptText(context.Background(), "x"); !errors.Is(err, want) {
		t.Fatalf("error=%v; want %v", err, want)
	}
	if a.State().IsStreaming {
		t.Fatal("run not settled")
	}
}
