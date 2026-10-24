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

func TestCheckCompactionDisabled(t *testing.T) {
	var runs atomic.Int64
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary text that is long enough to pass the usability check"), smallCompactionSettings())
	seedBranch(t, session)
	session.control.autoCompaction = false

	assistant := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: 1000,
		StopReason: ai.StopStop, Usage: ai.Usage{Input: 99000, TotalTokens: 99000},
	}
	compacted, err := session.CheckCompaction(ctxpkg.Background(), assistant, true)
	if err != nil || compacted {
		t.Fatalf("compacted = %v err = %v", compacted, err)
	}
	if runs.Load() != 0 {
		t.Fatalf("runs = %d", runs.Load())
	}
	for _, entry := range session.Sessions.GetEntries() {
		if entry.Type == "compaction" {
			t.Fatal("no compaction entry expected")
		}
	}
}

func TestCheckCompactionThreshold(t *testing.T) {
	var runs atomic.Int64
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary text that is long enough to pass the usability check"), smallCompactionSettings())
	seedBranch(t, session)

	assistant := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopStop,
		Usage:      ai.Usage{Input: 99950, Output: 10, TotalTokens: 99960},
	}
	compacted, err := session.CheckCompaction(ctxpkg.Background(), assistant, true)
	if err != nil {
		t.Fatal(err)
	}
	// Threshold compaction never retries.
	if compacted {
		t.Fatal("threshold compaction must not continue the agent")
	}
	if runs.Load() != 1 {
		t.Fatalf("runs = %d", runs.Load())
	}
	var found *SessionEntry
	entries := session.Sessions.GetEntries()
	for index := range entries {
		if entries[index].Type == "compaction" {
			found = &entries[index]
		}
	}
	if found == nil || !strings.Contains(found.Summary, "summary text") {
		t.Fatalf("entries = %+v", entries)
	}
	if len(session.Messages()) == 0 {
		t.Fatal("agent messages must be refreshed")
	}

	// A message from before the compaction is stale and skipped.
	stale, err := session.CheckCompaction(ctxpkg.Background(), assistant, true)
	if err != nil || stale {
		t.Fatalf("stale check = %v err = %v", stale, err)
	}
}

func TestCheckCompactionOverflowCompleted(t *testing.T) {
	var runs atomic.Int64
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "overflow summary text that passes the usability check too"), smallCompactionSettings())
	seedBranch(t, session)

	// Case 2: a successful response beyond the window compacts without retry.
	completed := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopStop,
		Usage:      ai.Usage{Input: 2000, TotalTokens: 2000},
	}
	compacted, err := session.CheckCompaction(ctxpkg.Background(), completed, true)
	if err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("completed overflow must not continue")
	}
	if runs.Load() != 1 {
		t.Fatalf("runs = %d", runs.Load())
	}
}

func TestCheckCompactionOverflowError(t *testing.T) {
	var runs atomic.Int64
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "recovered summary text that passes the usability check"), smallCompactionSettings())
	seedBranch(t, session)

	// Case 1: an error overflow drops the failed message, compacts, and retries.
	failed := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopError, ErrorMessage: strPtr("prompt is too long: 2000 tokens > 1000 maximum"),
	}
	messages := append(session.Agent.State().Messages, ai.Message(failed))
	session.Agent.SetMessages(messages)
	// The normal event flow persists the assistant and records its entry id; the
	// recovery omission needs that source entry to undo it from context.
	failedEntryID := session.Sessions.AppendMessage(failed)
	session.entryIDsByMessage = map[ai.Message]string{ai.Message(failed): failedEntryID}

	compacted, err := session.CheckCompaction(ctxpkg.Background(), failed, true)
	if err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("overflow recovery must continue the agent")
	}
	// The omission leaves the failed assistant as the first kept entry, so the
	// cut is a split turn: the history and the turn prefix are summarized
	// separately. Upstream prepareCompaction returns the same shape for this
	// branch (firstKeptEntryId = the omitted assistant, isSplitTurn = true).
	if runs.Load() != 2 {
		t.Fatalf("runs = %d, want the split-turn pair", runs.Load())
	}
	// The failed message was removed from the post-compaction projection, not
	// only from agent state, so agent.continue can run.
	for _, message := range session.Messages() {
		if assistant, ok := message.(*ai.AssistantMessage); ok && assistant.StopReason == ai.StopError {
			t.Fatalf("failed message must be dropped: %+v", assistant)
		}
	}
	if len(session.Messages()) == 0 || ai.RoleOf(session.Messages()[len(session.Messages())-1]) == ai.RoleAssistant {
		t.Fatalf("post-compaction transcript must not end with an assistant: %+v", session.Messages())
	}
	var omission *SessionEntry
	for i := range session.Sessions.GetEntries() {
		entry := &session.Sessions.GetEntries()[i]
		if entry.Type == "context_edit" && entry.TargetID == failedEntryID && string(entry.Replacement) == "null" {
			omission = entry
		}
	}
	if omission == nil {
		t.Fatal("recovery omission context_edit entry not persisted")
	}

	// A second overflow cannot recover again.
	second := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS() + 1000,
		StopReason: ai.StopError, ErrorMessage: strPtr("prompt is too long: 2000 tokens > 1000 maximum"),
	}
	session.Agent.SetMessages([]ai.Message{second})
	events := []*SessionEvent{}
	session.Subscribe(func(event *SessionEvent) { events = append(events, event) })
	compacted, err = session.CheckCompaction(ctxpkg.Background(), second, true)
	if err != nil || compacted {
		t.Fatalf("compacted = %v err = %v", compacted, err)
	}
	var failure *SessionEvent
	for _, event := range events {
		if event.Type == SessionCompactionEnd && event.ErrorMessage != "" {
			failure = event
		}
	}
	if failure == nil || !strings.Contains(failure.ErrorMessage, "Context overflow recovery failed") {
		t.Fatalf("events = %+v", events)
	}
}

func TestCheckCompactionGuards(t *testing.T) {
	var runs atomic.Int64
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary text that is long enough to pass the usability check"), smallCompactionSettings())
	seedBranch(t, session)

	// Aborted messages are skipped when the check allows it.
	aborted := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopAborted,
	}
	if compacted, err := session.CheckCompaction(ctxpkg.Background(), aborted, true); err != nil || compacted {
		t.Fatalf("aborted = %v err = %v", compacted, err)
	}
	// The pre-prompt check includes aborted messages (usage decides); the small
	// usage does not cross the threshold.
	if compacted, err := session.CheckCompaction(ctxpkg.Background(), aborted, false); err != nil || compacted {
		t.Fatalf("aborted with usage = %v err = %v", compacted, err)
	}

	// A different model's overflow is ignored.
	other := &ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: "openai", Model: "other", Timestamp: timeNowMS(),
		StopReason: ai.StopError, ErrorMessage: strPtr("prompt is too long: 2000 tokens > 1000 maximum"),
	}
	if compacted, err := session.CheckCompaction(ctxpkg.Background(), other, true); err != nil || compacted {
		t.Fatalf("other model = %v err = %v", compacted, err)
	}
	if runs.Load() != 0 {
		t.Fatalf("runs = %d", runs.Load())
	}

	// A recoverable length stop (output below the model limit) compacts and
	// retries the turn.
	length := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopLength, Usage: ai.Usage{Input: 500, Output: 10, TotalTokens: 510},
	}
	if !ai.IsRecoverableLength(length, 100) {
		t.Fatal("length stop must be recoverable")
	}
	compacted, err := session.CheckCompaction(ctxpkg.Background(), length, true)
	if err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("recoverable length must continue")
	}
	if runs.Load() != 1 {
		t.Fatalf("runs = %d", runs.Load())
	}
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

// TestCheckCompactionProjectsContextEdits covers Case 3 when the branch carries
// a context edit: provider usage recorded before the edit describes an older
// context, so the projected estimate decides (upstream's hasContextEdits
// branch). The stale usage here would cross the threshold on its own.
func TestCheckCompactionProjectsContextEdits(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, unreachableStreamFn, smallCompactionSettings())
	seedBranch(t, session)

	entries := session.Sessions.GetEntries()
	if _, err := session.Sessions.AppendContextEdit(entries[0].ID, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	session.Agent.SetMessages(session.Sessions.Projection().Messages)

	assistant := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopStop,
		Usage:      ai.Usage{Input: 99950, Output: 10, TotalTokens: 99960},
	}
	compacted, err := session.CheckCompaction(ctxpkg.Background(), assistant, true)
	if err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("threshold compaction must not continue the agent")
	}
	if session.Sessions.LatestCompaction() != nil {
		t.Fatal("stale pre-edit usage triggered a compaction")
	}
}

// TestCheckCompactionIgnoresOmittedAssistant covers the projection guards on
// the overflow cases: an assistant a context edit removed from the projection
// cannot trigger recovery again, and silent overflow needs usage that still
// matches the projection (upstream assistantIsProjected /
// assistantUsageMatchesProjection).
func TestCheckCompactionIgnoresOmittedAssistant(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	session := compactionTestSession(t, model, unreachableStreamFn, smallCompactionSettings())
	seedBranch(t, session)

	failed := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopError, ErrorMessage: strPtr("prompt is too long: 2000 tokens > 1000 maximum"),
	}
	failedID := session.Sessions.AppendMessage(failed)
	session.entryIDsByMessage = map[ai.Message]string{ai.Message(failed): failedID}
	if _, err := session.Sessions.AppendContextEdit(failedID, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	session.Agent.SetMessages(session.Sessions.Projection().Messages)

	compacted, err := session.CheckCompaction(ctxpkg.Background(), failed, true)
	if err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("an omitted assistant must not trigger overflow recovery")
	}
	if session.Sessions.LatestCompaction() != nil {
		t.Fatal("an omitted assistant must not trigger compaction")
	}
}

// TestCheckCompactionRequiresProjectedUsage covers silent overflow: a
// successful response whose usage exceeds the window only counts when no
// context edit came after it, because the edit changed what the model saw.
func TestCheckCompactionRequiresProjectedUsage(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	session := compactionTestSession(t, model, unreachableStreamFn, smallCompactionSettings())
	seedBranch(t, session)

	entries := session.Sessions.GetEntries()
	usageAssistant := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", Timestamp: timeNowMS(),
		StopReason: ai.StopStop, Usage: ai.Usage{Input: 2000, TotalTokens: 2000},
	}
	// The response is persisted, then a context edit lands after it.
	usageID := session.Sessions.AppendMessage(usageAssistant)
	session.entryIDsByMessage = map[ai.Message]string{ai.Message(usageAssistant): usageID}
	if _, err := session.Sessions.AppendContextEdit(entries[0].ID, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	if !ai.IsContextOverflow(usageAssistant, model.ContextWindow) {
		t.Fatal("fixture must be a silent-overflow response")
	}
	compacted, err := session.CheckCompaction(ctxpkg.Background(), usageAssistant, true)
	if err != nil {
		t.Fatal(err)
	}
	if compacted || session.Sessions.LatestCompaction() != nil {
		t.Fatal("usage recorded before a context edit must not trigger silent-overflow compaction")
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
