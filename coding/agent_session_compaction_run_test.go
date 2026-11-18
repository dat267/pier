package coding

import (
	ctxpkg "context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Round 110 tests: the automatic compaction decision (overflow, recoverable
// length, threshold) and RunAutoCompaction.

// smallCompactionSettings keeps everything before the last turn summarizable.
func smallCompactionSettings() CompactionSettings {
	return CompactionSettings{Enabled: true, ReserveTokens: 100, KeepRecentTokens: 1}
}

func compactionTestSession(t *testing.T, model *ai.Model, streamFn agent.StreamFn, settings CompactionSettings) *AgentSession {
	t.Helper()
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Model: model, StreamFn: streamFn, Settings: SessionSettings{Compaction: settings},
	})
	if err != nil {
		t.Fatal(err)
	}
	session.control = &AgentSessionControl{autoCompaction: true, autoRetry: false}
	return session
}

// seedBranch appends two conversation turns to the session and mirrors them
// into agent state.
func seedBranch(t *testing.T, session *AgentSession) {
	t.Helper()
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first question"}})
	session.Sessions.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content:    ai.ContentList{ai.TextContent{Text: "first answer with some detail to tokenize"}},
		StopReason: ai.StopStop,
	})
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second question"}})
	session.Sessions.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content:    ai.ContentList{ai.TextContent{Text: "second answer"}},
		StopReason: ai.StopStop,
	})
	// Ending on a user message keeps the cut point on a turn start, so the
	// summary is a single LLM call.
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "third question"}})
	session.Agent.SetMessages(session.Sessions.Projection().Messages)
}

func TestAutoCompactionEndReportsContinuation(t *testing.T) {
	// v1.4.0 agent-session.ts _runAutoCompaction emits its willRetry argument.
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint(retry), func(t *testing.T) {
			var runs atomic.Int64
			model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
			session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary text that is long enough to pass the usability check"), smallCompactionSettings())
			seedBranch(t, session)
			var end *SessionEvent
			session.Subscribe(func(event *SessionEvent) {
				if event.Type == SessionCompactionEnd {
					end = event
				}
			})
			continued, err := session.RunAutoCompaction(ctxpkg.Background(), CompactionOverflow, retry)
			if err != nil {
				t.Fatal(err)
			}
			if continued != retry || end == nil || end.Result == nil || end.WillRetry != retry {
				t.Fatalf("continued=%v end=%+v, want willRetry=%v", continued, end, retry)
			}
		})
	}
}

func TestRunAutoCompactionWithoutModel(t *testing.T) {
	var runs atomic.Int64
	session := compactionTestSession(t, nil, summarizingStreamFn(t, &runs, "summary"), smallCompactionSettings())
	session.Agent.SetModel(nil)
	compacted, err := session.RunAutoCompaction(ctxpkg.Background(), CompactionThreshold, false)
	if err != nil || compacted || runs.Load() != 0 {
		t.Fatalf("compacted = %v err = %v runs = %d", compacted, err, runs.Load())
	}
}

func TestAbortCompaction(t *testing.T) {
	var runs atomic.Int64
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary text that is long enough to pass the usability check"), smallCompactionSettings())
	session.AbortCompaction()
	if session.IsCompacting() {
		t.Fatal("no compaction in flight")
	}
}

// summarizingStreamFn answers summarization calls with a fixed summary.
func summarizingStreamFn(t *testing.T, runs *atomic.Int64, text string) agent.StreamFn {
	t.Helper()
	return func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		runs.Add(1)
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: model.Provider, Model: model.ID,
				Content: ai.ContentList{ai.TextContent{Text: text}}, StopReason: ai.StopStop,
				Usage: ai.Usage{Input: 100, Output: 10, TotalTokens: 110},
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}
}

func unreachableStreamFn(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	panic("streamFn must not be called on this path")
}

// collectCompactionEvents subscribes and returns a function that drains the
// start/end events seen so far.
func collectCompactionEvents(session *AgentSession) func() (starts, ends int, lastEnd *SessionEvent) {
	var mu sync.Mutex
	starts, ends := 0, 0
	var lastEnd *SessionEvent
	session.Subscribe(func(event *SessionEvent) {
		mu.Lock()
		defer mu.Unlock()
		switch event.Type {
		case SessionCompactionStart:
			starts++
		case SessionCompactionEnd:
			ends++
			clone := *event
			lastEnd = &clone
		}
	})
	return func() (int, int, *SessionEvent) {
		mu.Lock()
		defer mu.Unlock()
		return starts, ends, lastEnd
	}
}

// TestCompactSessionTooSmallEmitsEnd asserts the impossible-compaction paths
// still emit compaction_end after compaction_start (upstream routes every
// error through the catch that emits compaction_end); missing it left the
// compaction status indicator mounted forever.
func TestCompactSessionTooSmallEmitsEnd(t *testing.T) {
	session := compactionTestSession(t, &ai.Model{ID: "m", Provider: "anthropic", API: ai.APIAnthropicMessages}, unreachableStreamFn, smallCompactionSettings())
	drain := collectCompactionEvents(session)

	_, err := session.CompactSession(ctxpkg.Background(), "")
	if err == nil {
		t.Fatal("expected an error for a too-small session")
	}
	starts, ends, lastEnd := drain()
	if starts != 1 || ends != 1 {
		t.Fatalf("events: %d starts, %d ends; want 1 of each", starts, ends)
	}
	if lastEnd == nil || !strings.Contains(lastEnd.ErrorMessage, "Nothing to compact") {
		t.Fatalf("last end %+v, want a Nothing-to-compact error message", lastEnd)
	}
	if !session.IsIdle() {
		t.Fatal("session must be idle after a failed compaction")
	}
}

// TestCompactSessionAlreadyCompactedEmitsEnd covers the already-compacted path.
func TestCompactSessionAlreadyCompactedEmitsEnd(t *testing.T) {
	session := compactionTestSession(t, &ai.Model{ID: "m", Provider: "anthropic", API: ai.APIAnthropicMessages}, unreachableStreamFn, smallCompactionSettings())
	drain := collectCompactionEvents(session)
	seedBranch(t, session)

	// Force a compaction entry so PrepareCompaction reports "already compacted".
	session.Sessions.AppendCompaction("summary of everything", "", 100, json.RawMessage("{}"), false, nil)

	_, err := session.CompactSession(ctxpkg.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "Already compacted") {
		t.Fatalf("error %v, want Already compacted", err)
	}
	starts, ends, lastEnd := drain()
	if starts != 1 || ends != 1 {
		t.Fatalf("events: %d starts, %d ends; want 1 of each", starts, ends)
	}
	if lastEnd == nil || !strings.Contains(lastEnd.ErrorMessage, "Already compacted") {
		t.Fatalf("last end %+v, want an Already-compacted error message", lastEnd)
	}
	if !session.IsIdle() {
		t.Fatal("session must be idle after a failed compaction")
	}
}

// TestCompactSessionNoModelEmitsEnd covers the missing-model path.
func TestCompactSessionNoModelEmitsEnd(t *testing.T) {
	session := compactionTestSession(t, nil, unreachableStreamFn, smallCompactionSettings())
	drain := collectCompactionEvents(session)

	_, err := session.CompactSession(ctxpkg.Background(), "")
	if err == nil {
		t.Fatal("expected an error without a model")
	}
	starts, ends, lastEnd := drain()
	if starts != 1 || ends != 1 {
		t.Fatalf("events: %d starts, %d ends; want 1 of each", starts, ends)
	}
	if lastEnd == nil || lastEnd.ErrorMessage == "" {
		t.Fatalf("last end %+v, want an error message", lastEnd)
	}
}

// TestIsCompactingDuringManualCompaction pins the activity state: a compaction
// in flight must report IsCompacting and must not report IsIdle. Upstream
// derives isCompacting from the abort controllers (agent-session.ts
// get isCompacting), so the state has one source; the port read a
// control.compactionActive flag that was never set, leaving the session
// "idle" for the whole compaction except while a branch summary was open.
func TestIsCompactingDuringManualCompaction(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, summarizingStreamFn(t, new(atomic.Int64), "unused"), smallCompactionSettings())
	seedBranch(t, session)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	session.CompactionStreamFn = func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		select {
		case started <- struct{}{}:
		default:
		}
		go func() {
			<-release
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: model.Provider, Model: model.ID,
				Content:    ai.ContentList{ai.TextContent{Text: "summary text long enough to be usable"}},
				StopReason: ai.StopStop, Usage: ai.Usage{Input: 100, Output: 10, TotalTokens: 110},
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}

	done := make(chan error, 1)
	go func() {
		_, err := session.CompactSession(ctxpkg.Background(), "")
		done <- err
	}()

	<-started
	if !session.IsCompacting() {
		t.Fatal("session must report compacting while a compaction is in flight")
	}
	if session.IsIdle() {
		t.Fatal("session must not report idle while compacting")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if session.IsCompacting() {
		t.Fatal("session still compacting after the compaction finished")
	}
	if !session.IsIdle() {
		t.Fatal("session must be idle after the compaction finished")
	}
}

// A completed response ends the one-attempt overflow recovery window (upstream
// resets _overflowRecoveryAttempted for any non-error, non-length stop), so a
// later overflow can recover again instead of failing outright.
func TestOverflowRecoveryResetsAfterSuccess(t *testing.T) {
	var runs atomic.Int64
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary text that is long enough to pass the usability check"), smallCompactionSettings())
	seedBranch(t, session)
	session.overflowRecovery.attempted = true

	success := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopStop,
	}
	session.handleAgentEvent(&agent.AgentEvent{Type: agent.MessageEnd, Message: success})
	if session.overflowRecovery.attempted {
		t.Fatal("a successful response must reset overflow recovery")
	}

	// A user message start also resets it (upstream message_start branch).
	session.overflowRecovery.attempted = true
	session.handleAgentEvent(&agent.AgentEvent{Type: agent.MessageStart, Message: &ai.UserMessage{Content: ai.StringOrBlocks{Text: "next"}}})
	if session.overflowRecovery.attempted {
		t.Fatal("a user message must reset overflow recovery")
	}
}

// TestCompactionReportsEstimatedTokensAfter covers the compaction_end payload:
// upstream recomputes the context size after appending the compaction
// (estimatedTokensAfter), and the port used to leave it at zero.
func TestCompactionReportsEstimatedTokensAfter(t *testing.T) {
	var runs atomic.Int64
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary text that is long enough to pass the usability check"), smallCompactionSettings())
	seedBranch(t, session)
	drain := collectCompactionEvents(session)

	assistant := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopStop,
		Usage:      ai.Usage{Input: 99950, Output: 10, TotalTokens: 99960},
	}
	if _, err := session.CheckCompaction(ctxpkg.Background(), assistant, true); err != nil {
		t.Fatal(err)
	}
	_, ends, lastEnd := drain()
	if ends != 1 || lastEnd == nil || lastEnd.Result == nil {
		t.Fatalf("compaction events = %d, %+v", ends, lastEnd)
	}
	result := lastEnd.Result
	if result.EstimatedTokensAfter <= 0 {
		t.Fatalf("estimatedTokensAfter = %d, want the post-compaction estimate", result.EstimatedTokensAfter)
	}
	if result.EstimatedTokensAfter > result.TokensBefore {
		t.Fatalf("estimatedTokensAfter = %d exceeds tokensBefore = %d", result.EstimatedTokensAfter, result.TokensBefore)
	}
}

func failingSummaryStreamFn(errorMessage string) agent.StreamFn {
	return func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: model.Provider, Model: model.ID,
				StopReason: ai.StopError, ErrorMessage: new(errorMessage),
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopError, Error: message})
		}()
		return stream
	}
}

// A failed auto-compaction names what failed: pi v1.5.0 (f9a26365)
// agent-session.ts _runAutoCompaction reports "Auto-compaction failed: <error>"
// for threshold compaction and "Context overflow recovery failed: <error>" for
// overflow recovery, and no message when the compaction was aborted. The port
// reported the bare summarization error, so the transcript showed a provider
// error with no hint that compaction produced it.
func TestAutoCompactionFailureNamesTheCompaction(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	for _, tc := range []struct {
		reason CompactionReason
		prefix string
	}{
		{CompactionThreshold, "Auto-compaction failed: "},
		{CompactionOverflow, "Context overflow recovery failed: "},
	} {
		session := compactionTestSession(t, model, failingSummaryStreamFn("429 rate limited"), smallCompactionSettings())
		seedBranch(t, session)
		events := []*SessionEvent{}
		session.Subscribe(func(event *SessionEvent) { events = append(events, event) })
		_, _ = session.RunAutoCompaction(ctxpkg.Background(), tc.reason, false)
		var end *SessionEvent
		for _, event := range events {
			if event.Type == SessionCompactionEnd {
				end = event
			}
		}
		if end == nil || end.Aborted || !strings.HasPrefix(end.ErrorMessage, tc.prefix) || !strings.Contains(end.ErrorMessage, "429 rate limited") {
			t.Fatalf("%s end = %+v", tc.reason, end)
		}
	}

	// An aborted compaction reports no error message.
	session := compactionTestSession(t, model, failingSummaryStreamFn("aborted"), smallCompactionSettings())
	seedBranch(t, session)
	events := []*SessionEvent{}
	session.Subscribe(func(event *SessionEvent) { events = append(events, event) })
	ctx, cancel := ctxpkg.WithCancel(ctxpkg.Background())
	session.Subscribe(func(event *SessionEvent) {
		if event.Type == SessionCompactionStart {
			cancel()
		}
	})
	_, _ = session.RunAutoCompaction(ctx, CompactionThreshold, false)
	for _, event := range events {
		if event.Type == SessionCompactionEnd && (!event.Aborted || event.ErrorMessage != "") {
			t.Fatalf("aborted end = %+v", event)
		}
	}
}
