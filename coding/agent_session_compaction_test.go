package coding

import (
	ctxpkg "context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/ai"
)

// Round 110 tests: the automatic compaction decision (overflow, recoverable
// length, threshold) and RunAutoCompaction.

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
		StopReason: ai.StopError, ErrorMessage: new("prompt is too long: 2000 tokens > 1000 maximum"),
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
		StopReason: ai.StopError, ErrorMessage: new("prompt is too long: 2000 tokens > 1000 maximum"),
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
		StopReason: ai.StopError, ErrorMessage: new("prompt is too long: 2000 tokens > 1000 maximum"),
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
		StopReason: ai.StopError, ErrorMessage: new("prompt is too long: 2000 tokens > 1000 maximum"),
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

// Upstream resolves the model that produced the message, not the selected one:
// `_modelForMessage` maps a virtual selection to the physical model the message
// names and uses that model's context window and maxTokens
// (core/agent-session.ts _checkCompaction). The port compared the message's
// provider and id against the selected (virtual) model, so under a virtual
// selection every message counted as a different model and the recoverable
// length recovery never ran.
func TestCheckCompactionResolvesPhysicalModelForVirtualSelection(t *testing.T) {
	var runs atomic.Int64
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		Credentials: ai.NewInMemoryCredentialStore(), DisableModelsJSON: true,
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	physical := runtime.GetModel("anthropic", "claude-haiku-4-5")
	if physical == nil || physical.MaxTokens == 0 {
		t.Fatal("catalog is missing the physical model")
	}
	virtual := &ai.Model{
		ID: "router", Name: "Router", API: VirtualModelAPI, Provider: "anthropic",
		ContextWindow: 1000000, MaxTokens: 128000, Reasoning: true,
	}
	// The summary is routed through the virtual model too, so the runtime needs a
	// registered router: an unregistered selection fails like the reference.
	if err := runtime.SetRuntimeAPIKey("anthropic", "synthetic-key", ctxpkg.Background()); err != nil {
		t.Fatalf("set key: %v", err)
	}
	if err := runtime.RegisterVirtualModel(VirtualModelDefinition{
		Provider: "anthropic", ID: "router", Name: "Router",
		Route: func(ModelRouteRequest) (ModelRoute, error) {
			return ModelRoute{Model: &ai.Model{Provider: physical.Provider, ID: physical.ID}, ThinkingLevel: "off"}, nil
		},
	}); err != nil {
		t.Fatalf("register router: %v", err)
	}
	session := compactionTestSession(t, virtual, summarizingStreamFn(t, &runs, "summary text that is long enough to pass the usability check"), smallCompactionSettings())
	session.control.ModelRuntime = runtime
	seedBranch(t, session)

	// A length stop below the physical model's output cap is recoverable.
	length := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: physical.ID,
		Timestamp: timeNowMS(), StopReason: ai.StopLength,
		Usage: ai.Usage{Input: 500, Output: 10, TotalTokens: 510},
	}
	compacted, err := session.CheckCompaction(ctxpkg.Background(), length, true)
	if err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("a recoverable length stop under a virtual selection must continue")
	}
	if runs.Load() != 1 {
		t.Fatalf("runs = %d", runs.Load())
	}
}
