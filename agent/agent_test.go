package agent

import (
	"context"
	"fmt"
	"github.com/dat267/pier/ai"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"weak"
)

// Port of the core subset of packages/agent/test/agent.test.ts.

func TestAgentDefaultState(t *testing.T) {
	agent, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	state := agent.State()
	if state.Model == nil || state.ThinkingLevel != ai.ThinkOff || state.Tools == nil ||
		state.Messages != nil || state.IsStreaming || state.StreamingMessage != nil ||
		state.PendingToolCalls == nil || len(state.PendingToolCalls) != 0 || state.ErrorMessage != "" {
		t.Fatalf("default state = %+v", state)
	}
}

func TestAgentCustomInitialState(t *testing.T) {
	customModel := &ai.Model{ID: "gpt-4o-mini", Name: "gpt-4o-mini", API: "openai-responses", Provider: "openai"}
	agent, err := NewAgent(&AgentOptions{
		StreamFn: unusedStreamFn(),
		InitialState: &AgentInitialState{
			SystemPrompt: "You are a helpful assistant.", Model: customModel, ThinkingLevel: ai.ThinkLow,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := agent.State()
	if len(state.Messages) != 1 {
		t.Fatalf("messages = %v", messageRoles(state.Messages))
	}
	sys := state.Messages[0].(*ai.SystemMessage)
	if sys.Content.Text != "You are a helpful assistant." || sys.Timestamp != 0 {
		t.Fatalf("initial system = %+v", sys)
	}
	if state.Model != customModel || state.ThinkingLevel != ai.ThinkLow {
		t.Fatalf("model/thinking = %v/%v", state.Model, state.ThinkingLevel)
	}
}

func TestAgentInitialToolsDeclared(t *testing.T) {
	tool := makeTool("echo")
	agent, err := NewAgent(&AgentOptions{
		StreamFn:     unusedStreamFn(),
		InitialState: &AgentInitialState{SystemPrompt: "You are helpful.", Tools: []AgentTool{tool}},
	})
	if err != nil {
		t.Fatal(err)
	}
	initial := agent.State().Messages[0].(*ai.SystemMessage)
	if initial.Content.Text != "You are helpful." {
		t.Fatalf("content = %q", initial.Content.Text)
	}
	if len(initial.ToolsAdded) != 1 || initial.ToolsAdded[0].Name != "echo" {
		t.Fatalf("toolsAdded = %+v", initial.ToolsAdded)
	}
}

func TestAgentDeclaresToolLoadoutChanges(t *testing.T) {
	// Upstream: requests record the declared-tool delta per call.
	var mu sync.Mutex
	var requests [][]string
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		mu.Lock()
		var entry []string
		for _, message := range context.Messages {
			if sys, ok := message.(*ai.SystemMessage); ok {
				added := "+"
				for _, tool := range sys.ToolsAdded {
					added += tool.Name + ","
				}
				removed := "-"
				for _, tool := range sys.ToolsRemoved {
					removed += tool.Name + ","
				}
				entry = append(entry, added, removed)
			}
		}
		requests = append(requests, entry)
		mu.Unlock()
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop,
			Message: createAssistantMessage(ai.ContentList{ai.TextContent{Text: "done"}}, ai.StopStop)})
		return stream
	}
	first, second := makeTool("first"), makeTool("second")
	agent, _ := NewAgent(&AgentOptions{
		InitialState: &AgentInitialState{SystemPrompt: "You are helpful.", Tools: []AgentTool{first}},
		StreamFn:     streamFn,
	})

	if err := agent.PromptText(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	agent.SetTools([]AgentTool{second})
	if err := agent.PromptText(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	if err := agent.PromptText(context.Background(), "three"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("requests = %v", requests)
	}
	// Upstream expectation: the first request carries the transcript's
	// initial declaration; the second announces the swap; the third no change.
	wantFirst := []string{"+first,", "-"}
	for i, v := range wantFirst {
		if requests[0][i] != v {
			t.Fatalf("requests[0] = %v; want %v", requests[0], wantFirst)
		}
	}
	var updates []*ai.SystemMessage
	for _, m := range agent.State().Messages {
		if sys, ok := m.(*ai.SystemMessage); ok && len(sys.ToolsRemoved) > 0 {
			updates = append(updates, sys)
		}
	}
	if len(updates) != 1 {
		t.Fatalf("updates = %d", len(updates))
	}
	if len(updates[0].ToolsAdded) != 1 || updates[0].ToolsAdded[0].Name != "second" ||
		len(updates[0].ToolsRemoved) != 1 || updates[0].ToolsRemoved[0].Name != "first" {
		t.Fatalf("update = %+v", updates[0])
	}
	// The initial declaration carries no executable fields.
	initial := agent.State().Messages[0].(*ai.SystemMessage)
	if len(initial.ToolsAdded) != 1 || initial.ToolsAdded[0].Description != "first tool" {
		t.Fatalf("initial = %+v", initial.ToolsAdded)
	}
}

func TestAgentResetRestoresBaseline(t *testing.T) {
	agent, _ := NewAgent(&AgentOptions{
		StreamFn:     unusedStreamFn(),
		InitialState: &AgentInitialState{SystemPrompt: "base", Tools: []AgentTool{makeTool("echo")}},
	})
	if err := agent.PromptText(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if err := agent.Reset(); err != nil {
		t.Fatal(err)
	}
	messages := agent.State().Messages
	if len(messages) != 1 || ai.RoleOf(messages[0]) != "system" {
		t.Fatalf("after reset = %v", messageRoles(messages))
	}
	sys := messages[0].(*ai.SystemMessage)
	if sys.Content.Text != "base" || len(sys.ToolsAdded) != 1 {
		t.Fatalf("baseline = %+v", sys)
	}
}

// D312: Go snapshots listeners before dispatch; an unsubscribe from an earlier
// callback takes effect on the next event. Upstream iterates its live Set, so
// removing a later listener during dispatch skips it for the current event.
func TestAgentUnsubscribeDuringDispatchUsesListenerSnapshot(t *testing.T) {
	agent, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	var unsubscribeSecond func()
	agent.Subscribe(func(event AgentEvent, _ context.Context) error {
		if event.Type == TurnStart {
			seen = append(seen, "first")
			unsubscribeSecond()
		}
		return nil
	})
	unsubscribeSecond = agent.Subscribe(func(event AgentEvent, _ context.Context) error {
		if event.Type == TurnStart {
			seen = append(seen, "second")
		}
		return nil
	})
	if err := agent.processEvents(AgentEvent{Type: TurnStart}, context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(seen, ","); got != "first,second" {
		t.Fatalf("listeners invoked = %q, want snapshot order", got)
	}
}

func TestAgentSubscribeAndLifecycle(t *testing.T) {
	streamFn, _ := mockStreamFn(createAssistantMessage(ai.ContentList{ai.TextContent{Text: "reply"}}, ai.StopStop))
	agent, _ := NewAgent(&AgentOptions{StreamFn: streamFn})

	var mu sync.Mutex
	var events []string
	cancel := agent.Subscribe(func(event AgentEvent, ctx context.Context) error {
		mu.Lock()
		events = append(events, event.Type)
		mu.Unlock()
		return nil
	})
	defer cancel()

	if err := agent.PromptText(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 || events[len(events)-1] != AgentEnd {
		t.Fatalf("events = %v", events)
	}
	// isStreaming false after settle.
	if agent.State().IsStreaming {
		t.Fatal("agent should be idle")
	}
	// Transcript grew with the assistant reply.
	roles := messageRoles(agent.State().Messages)
	if len(roles) != 2 || roles[1] != "assistant" {
		t.Fatalf("transcript = %v", roles)
	}
}

func TestAgentQueuesAndModes(t *testing.T) {
	streamFn, calls := mockStreamFn(
		createAssistantMessage(ai.ContentList{ai.TextContent{Text: "1"}}, ai.StopStop),
		createAssistantMessage(ai.ContentList{ai.TextContent{Text: "2"}}, ai.StopStop),
	)
	agent, _ := NewAgent(&AgentOptions{StreamFn: streamFn})

	agent.Steer(createUserMessage("steer-me"))
	if !agent.HasQueuedMessages() {
		t.Fatal("queue should have items")
	}
	// Queued steering is not yet in the transcript.
	if len(agent.State().Messages) != 0 {
		t.Fatal("queued message must not leak into state")
	}
	// The loop's AT-START steering poll injects it before the first response,
	// so one prompt run + one steering message = one LLM call.
	if err := agent.PromptText(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if agent.HasQueuedMessages() {
		t.Fatal("steering should have been drained")
	}
	if *calls != 1 {
		t.Fatalf("calls = %d; want 1", *calls)
	}

	// one-at-a-time: two queued steers drain one per poll.
	agent.Steer(createUserMessage("s1"))
	agent.Steer(createUserMessage("s2"))
	if agent.steeringQueue.getMode() != QueueModeOneAtATime {
		t.Fatal("default steering mode is one-at-a-time")
	}
	agent.SetSteeringMode(QueueModeAll)
	if agent.steeringQueue.getMode() != QueueModeAll {
		t.Fatal("mode setter failed")
	}
	agent.ClearAllQueues()
	if agent.HasQueuedMessages() {
		t.Fatal("queues should be cleared")
	}
}

func TestAgentContinueFromAssistantTail(t *testing.T) {
	// continue() with an assistant tail drains steering first.
	streamFn, calls := mockStreamFn(createAssistantMessage(ai.ContentList{ai.TextContent{Text: "r"}}, ai.StopStop))
	agent, _ := NewAgent(&AgentOptions{StreamFn: streamFn})

	// Seed transcript with an assistant tail via a prompt run.
	if err := agent.PromptText(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	agent.Steer(createUserMessage("queued"))
	if err := agent.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatalf("calls = %d; want 2", *calls)
	}
	roles := messageRoles(agent.State().Messages)
	// user(start), assistant, user(queued steer), assistant(reply).
	if len(roles) != 4 || roles[2] != "user" || roles[3] != "assistant" {
		t.Fatalf("transcript = %v", roles)
	}
	// A second continue with empty queues errors.
	if err := agent.Continue(context.Background()); err == nil ||
		err.Error() != "Cannot continue from message role: assistant" {
		t.Fatalf("continue error = %v", err)
	}
}

func TestAgentDoublePromptErrors(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	slowFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			<-block
			msg := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "slow"}}, ai.StopStop)
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: msg})
		}()
		return stream
	}
	agent, _ := NewAgent(&AgentOptions{StreamFn: slowFn})

	runErr := make(chan error, 1)
	go func() { runErr <- agent.PromptText(context.Background(), "first") }()
	time.Sleep(20 * time.Millisecond)

	if err := agent.PromptText(context.Background(), "second"); err == nil ||
		!strings.Contains(err.Error(), "already processing") {
		t.Fatalf("double prompt error = %v", err)
	}
	if err := agent.Reset(); err == nil {
		t.Fatal("reset during processing must error")
	}
	if err := agent.Continue(context.Background()); err == nil {
		t.Fatal("continue during processing must error")
	}
}

// pi v1.4.0 (4a080252), agent.ts PendingMessageQueue.drain replaces the
// queue with messages.slice(drained.length), releasing consumed slots. Once
// the transcript is replaced too, the delivered message must be collectible.
func TestDrainedAgentQueuesReleaseDeliveredMessages(t *testing.T) {
	for _, followUp := range []bool{false, true} {
		for _, mode := range []QueueMode{QueueModeOneAtATime, QueueModeAll} {
			t.Run(fmt.Sprintf("followUp=%t/mode=%s", followUp, mode), func(t *testing.T) {
				a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn(), SteeringMode: mode, FollowUpMode: mode})
				if err != nil {
					t.Fatal(err)
				}
				message := enqueueAgentRetentionMessage(a, followUp)
				if err := a.PromptText(context.Background(), "start"); err != nil {
					t.Fatal(err)
				}
				if a.HasQueuedMessages() {
					t.Fatal("run did not consume the queued message")
				}
				// Keeping it in the active transcript is legitimate. Replacing
				// that transcript must not leave an empty queue owning it.
				a.SetMessages(nil)
				runtime.GC()
				if message.Value() != nil {
					t.Fatal("empty queue retains a delivered message after transcript replacement")
				}
				runtime.KeepAlive(a)
			})
		}
	}
}

func enqueueAgentRetentionMessage(a *Agent, followUp bool) weak.Pointer[ai.UserMessage] {
	message := createUserMessage(strings.Repeat("queued", 1024))
	if followUp {
		a.FollowUp(message)
	} else {
		a.Steer(message)
	}
	return weak.Make(message)
}

// Reference: pi v1.4.0 agent.ts state/processEvents run on one JS thread.
// A Go snapshot must not expose the map that later tool events mutate.
func TestStatePendingCallsSnapshotIsIndependent(t *testing.T) {
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot AgentState
	a.Subscribe(func(e AgentEvent, _ context.Context) error {
		if e.Type == ToolExecutionStart {
			snapshot = a.State()
		}
		return nil
	})
	if err := a.processEvents(AgentEvent{Type: ToolExecutionStart, ToolCallID: "call"}, context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.processEvents(AgentEvent{Type: ToolExecutionEnd, ToolCallID: "call"}, context.Background()); err != nil {
		t.Fatal(err)
	}
	if !snapshot.PendingToolCalls["call"] {
		t.Fatal("tool end mutated the earlier snapshot")
	}
	delete(snapshot.PendingToolCalls, "call")
	if len(a.State().PendingToolCalls) != 0 {
		t.Fatal("unexpected active calls")
	}
}
