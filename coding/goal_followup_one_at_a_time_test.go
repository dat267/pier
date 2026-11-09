package coding

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/dat267/pier/ai"
)

// Alt+Enter queues follow-ups one at a time. A goal holds them until its final
// answer (D385), and releasing them must keep that granularity: each held
// follow-up becomes its own turn, not one request carrying all of them.
func TestGoalReleasedFollowUpsAreDeliveredOneAtATime(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	followUps := []string{"f1", "f2", "f3"}
	var session *AgentSession
	var requests [][]string
	round := 0
	streamFn := func(m *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		var users []string
		for _, message := range transcript.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				users = append(users, extractTextContent(user))
			}
		}
		requests = append(requests, users)
		round++
		if round == 2 {
			_, _ = session.GetToolDefinition("set_goal").Execute("complete", json.RawMessage(`{"status":"complete"}`), context.Background(), nil)
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "done"}},
			}})
		}()
		return stream
	}
	var err error
	session, err = NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, Goal: true, ConvertToLlm: ConvertToLlm,
	})
	if err != nil {
		t.Fatal(err)
	}
	var pending []func(context.Context) error
	session.Goal.SetStartWork(func(work func(context.Context) error) { pending = append(pending, work) })
	for _, text := range followUps {
		session.FollowUp(&ai.UserMessage{Content: ai.StringOrBlocks{Text: text}})
	}
	session.Goal.HandleCommand("set objective")
	// Run every released piece of work, including work it queues while running.
	for index := 0; index < len(pending); index++ {
		if err := pending[index](context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	assertOneFollowUpPerRequest(t, requests, followUps)
	if queued := session.GetFollowUpMessages(); len(queued) != 0 {
		t.Fatalf("follow-ups after the released runs = %v, want delivered", queued)
	}
}

// assertOneFollowUpPerRequest requires each request to deliver at most one new
// follow-up, all of them, in queue order. The transcript accumulates, so a
// later request legitimately re-sends the earlier follow-ups: the comparison is
// against the previous request.
func assertOneFollowUpPerRequest(t *testing.T, requests [][]string, followUps []string) {
	t.Helper()
	delivered := map[string]int{}
	previous := map[string]bool{}
	for index, users := range requests {
		present := map[string]bool{}
		for _, text := range followUps {
			for _, user := range users {
				if user == text {
					present[text] = true
				}
			}
		}
		added := 0
		for text := range present {
			if previous[text] {
				continue
			}
			added++
			delivered[text] = index
		}
		if added > 1 {
			t.Fatalf("request %d delivered %d new follow-ups (%v); each must be delivered on its own", index+1, added, users)
		}
		previous = present
	}
	if len(delivered) != len(followUps) {
		t.Fatalf("delivered follow-ups = %v, want all of %v", delivered, followUps)
	}
	previousIndex := -1
	for _, text := range followUps {
		if delivered[text] <= previousIndex {
			t.Fatalf("delivery order = %v, want the queue order %v", delivered, followUps)
		}
		previousIndex = delivered[text]
	}
}

// TestQueuedFollowUpsAreDeliveredOneAtATimeWithoutAGoal is the control for the
// release above: without a goal nothing holds the queue, and the run's own stop
// boundary already delivers one follow-up per turn (the default one-at-a-time
// mode). The handshake blocks the first request until every follow-up is
// queued, so the ordering under test is deterministic.
func TestQueuedFollowUpsAreDeliveredOneAtATimeWithoutAGoal(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	followUps := []string{"f1", "f2", "f3"}
	firstRequest := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var requests [][]string
	streamFn := func(m *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		var users []string
		for _, message := range transcript.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				users = append(users, extractTextContent(user))
			}
		}
		mu.Lock()
		requests = append(requests, users)
		first := len(requests) == 1
		mu.Unlock()
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			if first {
				once.Do(func() { close(firstRequest) })
				<-release
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "ack"}},
			}})
		}()
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, ConvertToLlm: ConvertToLlm,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.SetAutoCompactionEnabled(false)
	session.SetAutoRetryEnabled(false)
	if mode := session.FollowUpMode(); mode != QueueModeOneAtATime {
		t.Fatalf("follow-up mode = %q, want the one-at-a-time default", mode)
	}
	done := make(chan error, 1)
	go func() { done <- session.Prompt(context.Background(), "start", nil) }()
	<-firstRequest
	for _, text := range followUps {
		if err := session.Prompt(context.Background(), text, &PromptOptions{StreamingBehavior: "followUp"}); err != nil {
			t.Fatalf("queue %s: %v", text, err)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	assertOneFollowUpPerRequest(t, requests, followUps)
}

// TestQueuedFollowUpsFollowTheAllMode is the mode control: the release above
// must keep the configured granularity, so an explicit `all` still delivers the
// whole queue in one turn.
func TestQueuedFollowUpsFollowTheAllMode(t *testing.T) {
	manager, dir := newTestSession(t)
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	followUps := []string{"f1", "f2", "f3"}
	firstRequest := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var requests [][]string
	streamFn := func(m *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		var users []string
		for _, message := range transcript.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				users = append(users, extractTextContent(user))
			}
		}
		mu.Lock()
		requests = append(requests, users)
		first := len(requests) == 1
		mu.Unlock()
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			if first {
				once.Do(func() { close(firstRequest) })
				<-release
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "ack"}},
			}})
		}()
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, ConvertToLlm: ConvertToLlm,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.SetAutoCompactionEnabled(false)
	session.SetAutoRetryEnabled(false)
	session.SetFollowUpMode(QueueModeAll)
	done := make(chan error, 1)
	go func() { done <- session.Prompt(context.Background(), "start", nil) }()
	<-firstRequest
	for _, text := range followUps {
		if err := session.Prompt(context.Background(), text, &PromptOptions{StreamingBehavior: "followUp"}); err != nil {
			t.Fatalf("queue %s: %v", text, err)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	// The second request carries the whole queue: the mode is the caller's choice.
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want the prompt plus one batched continuation", len(requests))
	}
	last := requests[1]
	for _, text := range followUps {
		found := false
		for _, user := range last {
			if user == text {
				found = true
			}
		}
		if !found {
			t.Fatalf("batched request = %v, missing %q", last, text)
		}
	}
}
