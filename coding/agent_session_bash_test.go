package coding

import (
	ctxpkg "context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// waitForBashEvent polls for a bash-execution-update event with the id.
func waitForBashEvent(mu *sync.Mutex, events *[]*SessionEvent, id string, timeoutMS int) bool {
	deadline := time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, event := range *events {
			if event.Type == SessionBashExecutionUpdate && event.ID == id {
				mu.Unlock()
				return true
			}
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	return false
}

type atomicInt64 = atomic.Int64

// Round 111 tests: session bash execution and the manual compaction
// orchestration.

func TestSessionExecuteBash(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Model: model, StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	var deltasMu sync.Mutex
	var deltas []string
	result, err := session.ExecuteBash(ctxpkg.Background(), "printf hello", func(chunk string) {
		deltasMu.Lock()
		defer deltasMu.Unlock()
		deltas = append(deltas, chunk)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "hello" {
		t.Fatalf("output = %q", result.Output)
	}
	if len(deltas) == 0 {
		t.Fatal("no output chunks streamed")
	}
	// The result is recorded as a message entry with the original command.
	entries := session.Sessions.GetEntries()
	last := entries[len(entries)-1]
	if last.Type != "message" || !strings.Contains(string(last.Message), "printf hello") {
		t.Fatalf("entry = %+v", last)
	}
	// The update events fired with the execution id. Events are emitted from
	// the chunk reader goroutine, so the collector is mutex-guarded.
	var eventsMu sync.Mutex
	var events []*SessionEvent
	session.Subscribe(func(event *SessionEvent) {
		eventsMu.Lock()
		defer eventsMu.Unlock()
		events = append(events, event)
	})
	if _, err := session.ExecuteBash(ctxpkg.Background(), "echo run-two", nil, &ExecuteBashOptions{ID: "run-2"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for {
		eventsMu.Lock()
		for _, event := range events {
			if event.Type == SessionBashExecutionUpdate && event.ID == "run-2" {
				found = true
			}
		}
		eventsMu.Unlock()
		if found {
			break
		}
		// The chunk reader may deliver the last events just after the call
		// returns; give it a moment before failing.
		if !waitForBashEvent(&eventsMu, &events, "run-2", 50) {
			eventsMu.Lock()
			t.Fatalf("events = %+v", events)
		}
	}
}

// pi v1.3.0 core/agent-session.ts passes each tracked controller's signal
// to executeBashWithOperations; abortBash must reach that backend.
type abortObservingBash struct {
	started   chan struct{}
	release   chan struct{}
	cancelled chan bool
}

func (b abortObservingBash) Exec(ctx ctxpkg.Context, command, cwd string, options BashExecOptions) (*int, error) {
	close(b.started)
	<-b.release
	select {
	case <-options.Signal:
		b.cancelled <- true
	default:
		b.cancelled <- false
	}
	code := 0
	return &code, nil
}

func TestSessionAbortBashReachesExecution(t *testing.T) {
	persist := false
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), StreamFn: stubStreamFn,
		Sessions: NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	backend := abortObservingBash{make(chan struct{}), make(chan struct{}), make(chan bool, 1)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, err := session.ExecuteBash(ctxpkg.Background(), "probe", nil, &ExecuteBashOptions{Operations: backend})
		if err != nil {
			t.Error(err)
			return
		}
		if !result.Cancelled {
			t.Error("execution result must report cancellation")
		}
	}()
	<-backend.started
	session.AbortBash()
	close(backend.release)
	cancelled := <-backend.cancelled
	<-done
	if !cancelled {
		t.Fatal("AbortBash did not cancel the backend signal")
	}
}

func TestSessionExecuteBashDeferredWhileStreaming(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Model: model, StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Fake a streaming run: results are deferred and flushed afterwards.
	session.prompt().mu.Lock()
	session.prompt().runActive = true
	session.prompt().mu.Unlock()

	if _, err := session.ExecuteBash(ctxpkg.Background(), "echo hi", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !session.HasPendingBashMessages() {
		t.Fatal("streaming results must be deferred")
	}
	session.prompt().mu.Lock()
	session.prompt().runActive = false
	session.prompt().mu.Unlock()
	session.FlushPendingBashMessages()
	entries := session.Sessions.GetEntries()
	last := entries[len(entries)-1]
	if !strings.Contains(string(last.Message), "echo hi") {
		t.Fatalf("flushed entry = %+v", last)
	}
}

func TestSessionExecuteBashExcludeFromContext(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Model: model, StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ExecuteBash(ctxpkg.Background(), "echo secret", nil, &ExecuteBashOptions{ExcludeFromContext: true}); err != nil {
		t.Fatal(err)
	}
	entries := session.Sessions.GetEntries()
	last := entries[len(entries)-1]
	if !strings.Contains(string(last.Message), `"excludeFromContext":true`) {
		t.Fatalf("entry = %s", last.Message)
	}
}

func TestSessionCompactManual(t *testing.T) {
	var runs atomicCounter
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs.n, "manual summary"), smallCompactionSettings())
	seedBranch(t, session)

	events := []*SessionEvent{}
	session.Subscribe(func(event *SessionEvent) { events = append(events, event) })

	result, err := session.CompactSession(ctxpkg.Background(), "focus on the tests")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "manual summary") {
		t.Fatalf("summary = %q", result.Summary)
	}
	var start, end *SessionEvent
	for _, event := range events {
		switch event.Type {
		case SessionCompactionStart:
			start = event
		case SessionCompactionEnd:
			end = event
		}
	}
	if start == nil || start.Reason != CompactionManual {
		t.Fatalf("start = %+v", start)
	}
	if end == nil || end.Reason != CompactionManual || end.Result == nil || end.Aborted {
		t.Fatalf("end = %+v", end)
	}
	// The compaction entry is persisted and the agent state refreshed.
	var entry *SessionEntry
	entries := session.Sessions.GetEntries()
	for index := range entries {
		if entries[index].Type == "compaction" {
			entry = &entries[index]
		}
	}
	if entry == nil || !strings.Contains(entry.Summary, "manual summary") {
		t.Fatalf("entries = %+v", entries)
	}
	if len(session.Messages()) == 0 {
		t.Fatal("agent messages must be refreshed")
	}
	if session.IsCompacting() {
		t.Fatal("compaction must be finished")
	}
}

func TestSessionCompactAlreadyCompacted(t *testing.T) {
	var runs atomicCounter
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs.n, "summary"), smallCompactionSettings())
	seedBranch(t, session)
	if _, err := session.CompactSession(ctxpkg.Background(), ""); err != nil {
		t.Fatal(err)
	}
	// A second compaction reports that the session is already compacted.
	_, err := session.CompactSession(ctxpkg.Background(), "")
	if err == nil || err.Error() != "Already compacted" {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionCompactTooSmall(t *testing.T) {
	var runs atomicCounter
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs.n, "summary"), smallCompactionSettings())
	// A single user message cannot be compacted: there is nothing before the
	// kept recent turn.
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "only"}})
	if _, err := session.CompactSession(ctxpkg.Background(), ""); err == nil ||
		err.Error() != "Nothing to compact (session too small)" {
		t.Fatalf("err = %v", err)
	}
	if runs.n.Load() != 0 {
		t.Fatalf("runs = %d", runs.n.Load())
	}
}

func TestSessionCompactWithoutModel(t *testing.T) {
	var runs atomicCounter
	session := compactionTestSession(t, nil, summarizingStreamFn(t, &runs.n, "summary"), smallCompactionSettings())
	session.Agent.SetModel(nil)
	_, err := session.CompactSession(ctxpkg.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "No model selected.") {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionCompactAbortsActiveRun(t *testing.T) {
	var runs atomicCounter
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs.n, "summary"), smallCompactionSettings())
	seedBranch(t, session)
	// Compaction aborts an in-flight retry backoff; without one the counter is
	// preserved (upstream resets it only when the sleeping retry observes the
	// abort).
	session.mu.Lock()
	session.retryAttempt = 1
	session.mu.Unlock()
	if _, err := session.CompactSession(ctxpkg.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if session.RetryAttempt() != 1 {
		t.Fatalf("attempt = %d", session.RetryAttempt())
	}
}

// countingInt is a shared counter for stream functions.
type atomicCounter struct{ n atomicInt64 }

func TestSessionCompactEventOnError(t *testing.T) {
	// A stream function that fails surfaces the failure through compaction_end.
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	failing := func(failModel *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: failModel.Provider, Model: failModel.ID,
				StopReason: ai.StopError, ErrorMessage: strPtr("summarizer exploded"),
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopError, Error: message})
		}()
		return stream
	}
	session := compactionTestSession(t, model, failing, smallCompactionSettings())
	seedBranch(t, session)
	events := []*SessionEvent{}
	session.Subscribe(func(event *SessionEvent) { events = append(events, event) })
	_, err := session.CompactSession(ctxpkg.Background(), "")
	if err == nil {
		t.Fatal("compaction must fail")
	}
	var end *SessionEvent
	for _, event := range events {
		if event.Type == SessionCompactionEnd {
			end = event
		}
	}
	if end == nil || end.Result != nil || !strings.Contains(end.ErrorMessage, "Compaction failed") {
		t.Fatalf("end = %+v", end)
	}
}

// slowBash records when it runs and finishes after a delay.
type slowBash struct{ delay time.Duration }

func (b slowBash) Exec(ctx ctxpkg.Context, command, cwd string, options BashExecOptions) (*int, error) {
	time.Sleep(b.delay)
	code := 0
	return &code, nil
}

// The recorded message is stamped when the command started. The replayed
// transcript anchors the frame at that point among the turn's entries (D248),
// so stamping the completion would move it after everything the command ran
// alongside.
func TestSessionBashMessageIsStampedAtStart(t *testing.T) {
	session, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), StreamFn: stubStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	if _, err := session.ExecuteBash(ctxpkg.Background(), "probe", nil,
		&ExecuteBashOptions{Operations: slowBash{delay: 60 * time.Millisecond}}); err != nil {
		t.Fatal(err)
	}
	entries := session.Sessions.GetEntries()
	last := entries[len(entries)-1]
	message, err := ai.UnmarshalMessage(last.Message)
	if err != nil {
		t.Fatal(err)
	}
	custom, ok := message.(*ai.CustomMessage)
	if !ok || custom.Role != RoleBashExecution {
		t.Fatalf("last entry = %+v", last)
	}
	appendedAt, err := time.Parse(time.RFC3339Nano, last.Timestamp)
	if err != nil {
		t.Fatal(err)
	}
	if gave := appendedAt.UnixMilli() - custom.Timestamp; gave < 50 {
		t.Fatalf("message stamped %dms before the entry was appended (want the 60ms run): the stamp is the completion", gave)
	}
}

// The recorded duration is part of the message payload, so a reopened session
// can show the manual command's "Took" (D249).
func TestSessionBashMessageRecordsDuration(t *testing.T) {
	session, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), StreamFn: stubStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	if _, err := session.ExecuteBash(ctxpkg.Background(), "probe", nil,
		&ExecuteBashOptions{Operations: slowBash{delay: 60 * time.Millisecond}}); err != nil {
		t.Fatal(err)
	}
	entries := session.Sessions.GetEntries()
	last := entries[len(entries)-1]
	message, err := ai.UnmarshalMessage(last.Message)
	if err != nil {
		t.Fatal(err)
	}
	custom, ok := message.(*ai.CustomMessage)
	if !ok || custom.Role != RoleBashExecution {
		t.Fatalf("last entry = %+v", last)
	}
	var payload struct {
		DurationMS *int64 `json:"durationMs"`
	}
	if err := json.Unmarshal(custom.Content, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DurationMS == nil || *payload.DurationMS < 50 {
		t.Fatalf("durationMS = %v, want the 60ms run", payload.DurationMS)
	}
}
