package interactive

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// eventTestSession implements EventSession.
type eventTestSession struct {
	retryAttempt      int
	abortRetryCalls   int
	abortCompactCalls int
	thinking          ai.ThinkingLevel
}

func (s *eventTestSession) ThinkingLevel() ai.ThinkingLevel { return s.thinking }
func (s *eventTestSession) RetryAttempt() int               { return s.retryAttempt }
func (s *eventTestSession) AbortRetry()                     { s.abortRetryCalls++ }
func (s *eventTestSession) AbortCompaction()                { s.abortCompactCalls++ }
func (s *eventTestSession) IsStreaming() bool               { return false }

func newEventTestDispatcher(t *testing.T) (*EventDispatcher, *eventTestSession, *TranscriptRenderer, *InteractiveUIState) {
	t.Helper()
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	settings := coding.NewInMemorySettingsManager(nil, coding.SettingsManagerCreateOptions{})
	chat := &tui.Container{}
	transcript := NewTranscriptRenderer(chat, nil, settings, nil, nil)
	screen := tui.NewMainScreen(&fakeRendererTerminal{width: 80, height: 24}, false, "")
	uiState := NewInteractiveUIState(screen)
	uiState.StatusContainer = &tui.Container{}
	session := &eventTestSession{}
	dispatcher := NewEventDispatcher(transcript, uiState, nil, settings, session, nil, nil)
	return dispatcher, session, transcript, uiState
}

func agentEvent(eventType string, message ai.Message) *agent.AgentEvent {
	return &agent.AgentEvent{Type: eventType, Message: message}
}

// TestEventDispatcherLifecycle covers the agent/turn lifecycle events.
func TestEventDispatcherLifecycle(t *testing.T) {
	dispatcher, _, _, uiState := newEventTestDispatcher(t)

	initCalls := 0
	dispatcher.Init = func() { initCalls++ }
	progress := []bool{}
	dispatcher.TerminalProgress = func(active bool) { progress = append(progress, active) }

	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	if initCalls != 1 {
		t.Fatalf("init calls = %d", initCalls)
	}
	// A second event does not re-init.
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentStart})
	if initCalls != 1 {
		t.Fatalf("init calls = %d", initCalls)
	}

	dispatcher.Settings.SetShowTerminalProgress(true)
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionTurnStart})
	if uiState.ActiveStatusIndicator == nil || uiState.ActiveStatusIndicator.IndicatorKind() != StatusWorking {
		t.Fatal("working indicator not shown")
	}
	uiState.WorkingVisible = false
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionTurnStart})
	if uiState.ActiveStatusIndicator != nil {
		t.Fatal("working indicator not cleared")
	}
	uiState.WorkingVisible = true

	settled := 0
	dispatcher.CheckShutdownRequested = func() { settled++ }
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentSettled})
	if settled != 1 {
		t.Fatalf("settled = %d", settled)
	}

	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd})
	if got := progress; len(got) != 3 || got[0] != true || got[1] != true || got[2] != false {
		t.Fatalf("progress = %v", got)
	}
}

// TestEventDispatcherMessages covers the message lifecycle.
func TestEventDispatcherMessages(t *testing.T) {
	dispatcher, _, transcript, _ := newEventTestDispatcher(t)

	// User message start renders into the chat.
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type:  coding.SessionMessageStart,
		Agent: agentEvent("message_start", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}),
	})
	if len(transcript.Chat.Children) != 1 {
		t.Fatalf("children = %d", len(transcript.Chat.Children))
	}

	// Assistant start creates the streaming component.
	assistant := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "hi"}}, StopReason: ai.StopStop}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type:  coding.SessionMessageStart,
		Agent: agentEvent("message_start", assistant),
	})
	if dispatcher.StreamingComponent() == nil {
		t.Fatal("streaming component missing")
	}
	if transcript.StreamingComponent != dispatcher.StreamingComponent() {
		t.Fatal("transcript streaming component not synced")
	}

	// A message update with a tool call adds a tool component.
	arguments, _ := json.Marshal(map[string]any{"command": "ls"})
	withTool := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.TextContent{Text: "running"}, ai.ToolCall{ID: "t1", Name: "bash", Arguments: arguments}},
		StopReason: ai.StopStop,
	}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type:  coding.SessionMessageUpdate,
		Agent: agentEvent("message_update", withTool),
	})
	if len(dispatcher.PendingTools()) != 1 {
		t.Fatalf("pending = %d", len(dispatcher.PendingTools()))
	}
	// A second update with different args reuses the component.
	moreArgs, _ := json.Marshal(map[string]any{"command": "ls -la"})
	updated := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.ToolCall{ID: "t1", Name: "bash", Arguments: moreArgs}},
		StopReason: ai.StopStop,
	}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type:  coding.SessionMessageUpdate,
		Agent: agentEvent("message_update", updated),
	})
	if len(dispatcher.PendingTools()) != 1 {
		t.Fatalf("pending after update = %d", len(dispatcher.PendingTools()))
	}

	// message_end clears the streaming state.
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type:  coding.SessionMessageEnd,
		Agent: agentEvent("message_end", updated),
	})
	if dispatcher.StreamingComponent() != nil {
		t.Fatal("streaming component not cleared")
	}
	// A successful stop keeps the tools pending until their result arrives
	// (upstream only marks the args complete).
	if len(dispatcher.PendingTools()) != 1 {
		t.Fatalf("pending after end = %d", len(dispatcher.PendingTools()))
	}
	lines := strings.Join(renderChat(t, transcript.Chat), "\n")
	if !strings.Contains(lines, "ls -la") {
		t.Fatalf("tool render = %q", lines)
	}

	// User message_end is ignored.
	before := len(transcript.Chat.Children)
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type:  coding.SessionMessageEnd,
		Agent: agentEvent("message_end", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "x"}}),
	})
	if len(transcript.Chat.Children) != before {
		t.Fatal("user message_end changed the chat")
	}
}

// TestEventDispatcherAbortedMessage covers the aborted/error synthesis.
func TestEventDispatcherAbortedMessage(t *testing.T) {
	dispatcher, session, transcript, _ := newEventTestDispatcher(t)
	session.retryAttempt = 3

	assistant := &ai.AssistantMessage{Content: ai.ContentList{}, StopReason: ai.StopStop}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionMessageStart, Agent: agentEvent("message_start", assistant),
	})
	arguments, _ := json.Marshal(map[string]any{"command": "ls"})
	withTool := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.ToolCall{ID: "t1", Name: "bash", Arguments: arguments}},
		StopReason: ai.StopAborted,
	}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionMessageUpdate, Agent: agentEvent("message_update", withTool),
	})
	if len(dispatcher.PendingTools()) != 1 {
		t.Fatalf("pending = %d", len(dispatcher.PendingTools()))
	}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", withTool),
	})
	if len(dispatcher.PendingTools()) != 0 {
		t.Fatal("pending tools not cleared on abort")
	}
	lines := strings.Join(renderChat(t, transcript.Chat), "\n")
	if !strings.Contains(lines, "Aborted after 3 retry attempts") {
		t.Fatalf("aborted render = %q", lines)
	}

}

// TestEventDispatcherToolExecution covers the tool execution events.
func TestEventDispatcherToolExecution(t *testing.T) {
	dispatcher, _, transcript, _ := newEventTestDispatcher(t)

	arguments, _ := json.Marshal(map[string]any{"command": "ls"})
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionToolExecutionStart,
		Agent: &agent.AgentEvent{
			Type: "tool_execution_start", ToolCallID: "t1", ToolName: "bash", Args: arguments,
		},
	})
	if len(dispatcher.PendingTools()) != 1 {
		t.Fatalf("pending = %d", len(dispatcher.PendingTools()))
	}

	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionToolExecutionUpdate,
		Agent: &agent.AgentEvent{
			Type: "tool_execution_update", ToolCallID: "t1",
			PartialResult: agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "partial"}}},
		},
	})
	lines := strings.Join(renderChat(t, transcript.Chat), "\n")
	if !strings.Contains(lines, "partial") {
		t.Fatalf("partial render = %q", lines)
	}

	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionToolExecutionEnd,
		Agent: &agent.AgentEvent{
			Type: "tool_execution_end", ToolCallID: "t1",
			Result:  agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "done"}}},
			IsError: false,
		},
	})
	if len(dispatcher.PendingTools()) != 0 {
		t.Fatal("pending tools not cleared")
	}
	lines = strings.Join(renderChat(t, transcript.Chat), "\n")
	if !strings.Contains(lines, "done") {
		t.Fatalf("result render = %q", lines)
	}
}

// TestEventDispatcherEntryAppended covers the entry_appended routing.
func TestEventDispatcherEntryAppended(t *testing.T) {
	dispatcher, _, transcript, _ := newEventTestDispatcher(t)
	dispatcher.Settings.SetShowCacheMissNotices(true)

	// A custom entry without a renderer is skipped.
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionEntryAppended, Entry: &coding.SessionEntry{Type: "custom", CustomType: "note"},
	})
	if len(transcript.Chat.Children) != 0 {
		t.Fatal("custom entry rendered without a renderer")
	}

	// A cache-warm usage entry renders a line.
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type:  coding.SessionEntryAppended,
		Entry: &coding.SessionEntry{Type: "usage", Kind: "cache_warm", Usage: &ai.Usage{Input: 1}},
	})
	if len(transcript.Chat.Children) != 2 { // spacer + text
		t.Fatalf("children = %d", len(transcript.Chat.Children))
	}

	// A custom_message entry with display renders a message.
	display := true
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionEntryAppended,
		Entry: &coding.SessionEntry{
			Type: "custom_message", CustomType: "note", Display: &display,
			Content: json.RawMessage(`"hello"`), Timestamp: "2026-01-01T00:00:00.000Z",
		},
	})
	if len(transcript.Chat.Children) != 3 {
		t.Fatalf("custom_message not rendered: children = %d", len(transcript.Chat.Children))
	}

	// A custom_message entry without display is skipped.
	hidden := false
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionEntryAppended,
		Entry: &coding.SessionEntry{
			Type: "custom_message", CustomType: "note", Display: &hidden,
			Content: json.RawMessage(`"secret"`), Timestamp: "2026-01-01T00:00:00.000Z",
		},
	})
	if len(transcript.Chat.Children) != 3 {
		t.Fatalf("hidden custom_message rendered: children = %d", len(transcript.Chat.Children))
	}
}

// TestEventDispatcherRetries covers the retry and summarization events.
func TestEventDispatcherRetries(t *testing.T) {
	dispatcher, session, _, uiState := newEventTestDispatcher(t)
	editor := NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{})
	dispatcher.Editor = editor
	baseEscape := func() {}
	editor.OnEscape = baseEscape

	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAutoRetryStart, Attempt: 1, MaxAttempts: 3, DelayMS: 100})
	if uiState.ActiveStatusIndicator == nil || uiState.ActiveStatusIndicator.IndicatorKind() != StatusRetry {
		t.Fatal("retry indicator not shown")
	}
	editor.OnEscape()
	if session.abortRetryCalls != 1 {
		t.Fatalf("abort retry calls = %d", session.abortRetryCalls)
	}

	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAutoRetryEnd, Attempt: 3, Success: false, ErrorMessage: "nope"})
	if uiState.ActiveStatusIndicator != nil {
		t.Fatal("retry indicator not cleared")
	}
	// The original escape handler is restored.
	restored := 0
	editor.OnEscape = func() { restored++ }
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAutoRetryStart, Attempt: 1, MaxAttempts: 3})
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAutoRetryEnd, Attempt: 1, Success: true})
	editor.OnEscape()
	if restored != 1 {
		t.Fatalf("restored handler calls = %d", restored)
	}

	// Summarization retry events drive the status indicators.
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionSummarizationRetryScheduled, Attempt: 1, MaxAttempts: 2, DelayMS: 50, ErrorMessage: "boom"})
	if uiState.ActiveStatusIndicator == nil || uiState.ActiveStatusIndicator.IndicatorKind() != StatusRetry {
		t.Fatal("summarization retry indicator not shown")
	}
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionSummarizationRetryAttemptStart, Source: "branchSummary"})
	if uiState.ActiveStatusIndicator == nil || uiState.ActiveStatusIndicator.IndicatorKind() != StatusBranchSummary {
		t.Fatalf("branch summary indicator = %v", uiState.ActiveStatusIndicator)
	}
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionSummarizationRetryAttemptStart, Source: "compaction", Reason: coding.CompactionThreshold})
	if uiState.ActiveStatusIndicator == nil || uiState.ActiveStatusIndicator.IndicatorKind() != StatusCompaction {
		t.Fatalf("compaction indicator = %v", uiState.ActiveStatusIndicator)
	}
	// summarization_retry_finished only clears a *retry* indicator; the
	// compaction/branch-summary indicator stays until its own end event
	// (upstream's kind filter behaves the same).
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionSummarizationRetryFinished})
	if uiState.ActiveStatusIndicator == nil || uiState.ActiveStatusIndicator.IndicatorKind() != StatusCompaction {
		t.Fatalf("indicator after finished = %v", uiState.ActiveStatusIndicator)
	}
}

// TestEventDispatcherCompaction covers the compaction events.
func TestEventDispatcherCompaction(t *testing.T) {
	dispatcher, session, transcript, uiState := newEventTestDispatcher(t)
	editor := NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{})
	dispatcher.Editor = editor

	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionCompactionStart, Reason: coding.CompactionManual})
	if uiState.ActiveStatusIndicator == nil || uiState.ActiveStatusIndicator.IndicatorKind() != StatusCompaction {
		t.Fatal("compaction indicator not shown")
	}
	editor.OnEscape()
	if session.abortCompactCalls != 1 {
		t.Fatalf("abort compaction calls = %d", session.abortCompactCalls)
	}

	errors := []string{}
	dispatcher.ShowError = func(message string) { errors = append(errors, message) }
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionCompactionEnd, Reason: coding.CompactionManual, Aborted: true})
	if uiState.ActiveStatusIndicator != nil {
		t.Fatal("compaction indicator not cleared")
	}
	if len(errors) != 1 || errors[0] != "Compaction cancelled" {
		t.Fatalf("errors = %v", errors)
	}

	// Auto-compaction cancellation shows a status instead.
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionCompactionStart, Reason: coding.CompactionThreshold})
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionCompactionEnd, Reason: coding.CompactionThreshold, Aborted: true})
	lines := strings.Join(renderChat(t, transcript.Chat), "\n")
	if !strings.Contains(lines, "Auto-compaction cancelled") {
		t.Fatalf("status = %q", lines)
	}

	// An error message is rendered in the chat for auto-compaction.
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionCompactionStart, Reason: coding.CompactionOverflow})
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionCompactionEnd, Reason: coding.CompactionOverflow, ErrorMessage: "overflow failed"})
	lines = strings.Join(renderChat(t, transcript.Chat), "\n")
	if !strings.Contains(lines, "overflow failed") {
		t.Fatalf("error render = %q", lines)
	}

	// A successful compaction rebuilds the chat from the session context. The
	// context's first entry must be the latest compaction.
	manager := coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: new(false)})
	manager.AppendMessage(ai.Message(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "old"}}))
	manager.AppendCompaction("earlier", "", 10, nil, false, nil)
	dispatcher.SessionInfo = manager
	dispatcher.Transcript.SessionInfo = manager
	flushed := []bool{}
	dispatcher.FlushCompactionQueue = func(willRetry bool) { flushed = append(flushed, willRetry) }
	borderUpdates := 0
	dispatcher.Transcript.UpdateEditorBorderColor = func() { borderUpdates++ }

	dispatcher.Settings.SetShowCacheMissNotices(true)
	// D406: the end event reports the run's measured duration.
	dispatcher.compactionStartedAt = time.Now().Add(-6*time.Minute - 18*time.Second)
	usage := ai.Usage{Input: 100, Cost: ai.UsageCost{Total: 0.5}}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionCompactionEnd, Reason: coding.CompactionManual,
		Result: &coding.CompactionResult{Summary: "summarized", TokensBefore: 500, Usage: &usage},
	})
	if len(flushed) != 1 || flushed[0] {
		t.Fatalf("flushed = %v", flushed)
	}
	if borderUpdates != 1 {
		t.Fatalf("compaction rebuild must refresh the editor border, got %d updates", borderUpdates)
	}
	lines = strings.Join(renderChat(t, transcript.Chat), "\n")
	if !strings.Contains(lines, "[compaction]") || !strings.Contains(lines, "Compaction: ") {
		t.Fatalf("compaction render = %q", lines)
	}
	if !strings.Contains(lines, "Compaction: 6m 18s · ") && !strings.Contains(lines, "Compaction: 6m 19s · ") {
		t.Fatalf("compaction render carries no elapsed time: %q", lines)
	}
}

// TestQueueUpdateRefreshesPendingBanner pins the steering-banner fix: when a
// queued steering/follow-up message is consumed by the turn, the session
// emits queue_update and the dispatcher must rebuild the pending-messages
// banner (upstream calls updatePendingMessagesDisplay). Without the hook the
// banner kept showing messages that were no longer queued.
func TestQueueUpdateRefreshesPendingBanner(t *testing.T) {
	dispatcher, _, _, _ := newEventTestDispatcher(t)

	refreshes := 0
	dispatcher.UpdatePendingMessagesDisplay = func() { refreshes++ }

	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionQueueUpdate})
	if refreshes != 1 {
		t.Fatalf("queue_update must refresh the pending banner, got %d refreshes", refreshes)
	}
}

// TestThinkingLevelReadsTheSessionState pins where the working indicator's
// border color comes from. Upstream reads session.thinkingLevel (defaulting to
// "off"); the port copied the whole session entry tree via SessionInfo
// GetEntries, discarded it, and always returned the empty level. SessionInfo is
// deliberately nil here: the answer must not depend on the entry tree.
func TestThinkingLevelReadsTheSessionState(t *testing.T) {
	dispatcher := &EventDispatcher{Session: &eventTestSession{thinking: "high"}}
	if got := dispatcher.thinkingLevel(); got != "high" {
		t.Fatalf("thinking level = %q; want high", got)
	}
	dispatcher.Session = &eventTestSession{}
	if got := dispatcher.thinkingLevel(); got != "off" {
		t.Fatalf("unset thinking level = %q; want off", got)
	}
	dispatcher.Session = nil
	if got := dispatcher.thinkingLevel(); got != "off" {
		t.Fatalf("missing session thinking level = %q; want off", got)
	}
}

// Upstream refreshes the terminal title on session_info_changed and the editor
// border color on thinking_level_changed.
func TestEventDispatcherTitleAndBorder(t *testing.T) {
	dispatcher := NewEventDispatcher(nil, nil, nil, nil, nil, nil, nil)
	titles, borders := 0, 0
	dispatcher.UpdateTerminalTitle = func() { titles++ }
	dispatcher.UpdateEditorBorderColor = func() { borders++ }
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionInfoChanged})
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionThinkingLevelChanged})
	if titles != 1 || borders != 1 {
		t.Fatalf("titles=%d borders=%d; want 1 and 1", titles, borders)
	}
}

// Tool results can carry images; the render payload keeps them (upstream
// getTextOutput reads text and image blocks).
func TestSortToolResultKeepsImages(t *testing.T) {
	result := sortToolResultFromAgent(agent.AgentToolResult{Content: []ai.Content{
		ai.TextContent{Text: "see"},
		ai.ImageContent{Data: "abc", MimeType: "image/png"},
	}})
	if len(result.Content) != 2 {
		t.Fatalf("content = %+v", result.Content)
	}
	if result.Content[1].Type != "image" || result.Content[1].MimeType != "image/png" || result.Content[1].Data != "abc" {
		t.Fatalf("image block = %+v", result.Content[1])
	}
}

// TestEventDispatcherUserMessageStartRefreshesPendingMessages covers the
// pending-message refresh on a user message_start (upstream addMessageToChat
// followed by updatePendingMessagesDisplay).
func TestEventDispatcherUserMessageStartRefreshesPendingMessages(t *testing.T) {
	dispatcher, _, _, _ := newEventTestDispatcher(t)
	calls := 0
	dispatcher.UpdatePendingMessagesDisplay = func() { calls++ }
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type:  coding.SessionMessageStart,
		Agent: agentEvent("message_start", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}),
	})
	if calls != 1 {
		t.Fatalf("pending message updates = %d", calls)
	}
}

// The abort notice is a display string: the dispatcher must not write it onto
// the assistant message, which the agent and the session still own (the write
// raced with turn_end and could land in the persisted session file).
func TestAbortedMessageDisplayDoesNotMutateTheEventMessage(t *testing.T) {
	dispatcher, session, transcript, _ := newEventTestDispatcher(t)
	session.retryAttempt = 2

	started := &ai.AssistantMessage{Content: ai.ContentList{}, StopReason: ai.StopPending}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionMessageStart, Agent: agentEvent("message_start", started),
	})

	aborted := &ai.AssistantMessage{
		Content:    ai.ContentList{ai.TextContent{Text: "stopped"}},
		StopReason: ai.StopAborted,
	}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", aborted),
	})

	if aborted.ErrorMessage != nil {
		t.Fatalf("the dispatcher wrote %q onto the message it was given", *aborted.ErrorMessage)
	}
	lines := strings.Join(renderChat(t, transcript.Chat), "\n")
	if !strings.Contains(lines, "Aborted after 2 retry attempts") {
		t.Fatalf("aborted render = %q", lines)
	}
}

// A displaced call's end event carries a handle, not an outcome: the dispatcher
// must keep the frame pending (so the call's later updates still land) and must
// not treat the handle as the result. A real end finalizes it.
func TestDispatcherKeepsABackgroundedFramePending(t *testing.T) {
	newRendererTestTheme(t)
	transcript := NewTranscriptRenderer(&tui.Container{}, nil, nil, nil, nil)
	dispatcher := NewEventDispatcher(transcript, nil, nil, nil, nil, nil, nil)
	component := NewToolExecutionComponent("bash", "c9", map[string]any{}, ToolExecutionOptions{},
		WithBuiltInRenderers("bash", nil), nil, "/tmp")
	component.MarkExecutionStarted()
	dispatcher.pendingTools["c9"] = component

	handle := json.RawMessage(`{"backgroundHandle":"bg-9","toolCallId":"c9","toolName":"bash","outputPath":"/tmp/x.log"}`)
	dispatcher.handleToolExecutionEnd(&coding.SessionEvent{Agent: &agent.AgentEvent{
		Type: agent.ToolExecutionEnd, ToolCallID: "c9", ToolName: "bash",
		Result: agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "moved"}}, Details: handle},
	}})
	if _, ok := dispatcher.pendingTools["c9"]; !ok {
		t.Fatal("a backgrounded call's frame was dropped, so its live output cannot land")
	}
	if !component.isPartial {
		t.Fatal("the handle was recorded as the frame's final result")
	}

	// The call's own stream keeps updating that frame.
	dispatcher.handleToolExecutionUpdate(&coding.SessionEvent{Agent: &agent.AgentEvent{
		Type: agent.ToolExecutionUpdate, ToolCallID: "c9", ToolName: "bash",
		PartialResult: agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "tick 1"}}},
	}})
	if !component.isPartial {
		t.Fatal("a live update finalized the frame")
	}

	// A real end finalizes and releases it.
	dispatcher.handleToolExecutionEnd(&coding.SessionEvent{Agent: &agent.AgentEvent{
		Type: agent.ToolExecutionEnd, ToolCallID: "c9", ToolName: "bash",
		Result: agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "done"}}},
	}})
	if _, ok := dispatcher.pendingTools["c9"]; ok {
		t.Fatal("a finished call's frame stayed pending")
	}
}

// A displaced call ends as a message, not as a tool_execution_end, so its finish
// notice is what stops its frame. The start notice names the same handle and
// must leave the frame ticking; a notice for another handle must not touch it.
func TestBackgroundNoticeClosesItsFrame(t *testing.T) {
	newRendererTestTheme(t)
	transcript := NewTranscriptRenderer(&tui.Container{}, nil, nil, nil, nil)
	dispatcher := NewEventDispatcher(transcript, nil, nil, nil, nil, nil, nil)
	component := NewToolExecutionComponent("bash", "c9", map[string]any{}, ToolExecutionOptions{},
		WithBuiltInRenderers("bash", nil), nil, "/tmp")
	component.MarkExecutionStarted()
	dispatcher.pendingTools["c9"] = component

	// The handle arrives as a status and the frame stays live.
	details := json.RawMessage(`{"backgroundHandle":"bg-9","toolCallId":"c9","toolName":"bash","outputPath":"/tmp/x.log"}`)
	dispatcher.handleToolExecutionEnd(&coding.SessionEvent{Agent: &agent.AgentEvent{
		Type: agent.ToolExecutionEnd, ToolCallID: "c9", ToolName: "bash",
		Result: agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "moved"}}, Details: details},
	}})
	if _, ok := dispatcher.pendingTools["c9"]; !ok {
		t.Fatal("the frame was released before the call finished")
	}

	noticeFor := func(text string) *coding.SessionEvent {
		return &coding.SessionEvent{Type: coding.SessionMessageStart, Agent: &agent.AgentEvent{
			Message: &ai.UserMessage{Content: ai.StringOrBlocks{Text: text}},
		}}
	}
	// Another call's notice must not touch this frame.
	if dispatcher.closeBackgroundFrameFromNotice(&ai.UserMessage{Content: ai.StringOrBlocks{
		Text: agent.StartedNotice(agent.ToolCallHandle{ID: "bg-other", ToolName: "bash"}).(*ai.UserMessage).Content.Text,
	}}) {
		t.Fatal("a notice for another handle closed this frame")
	}
	if _, ok := dispatcher.pendingTools["c9"]; !ok {
		t.Fatal("another call's notice released this frame")
	}

	// The start notice leaves it ticking.
	startText := agent.StartedNotice(agent.ToolCallHandle{ID: "bg-9", ToolName: "bash", OutputPath: "/tmp/x.log"}).(*ai.UserMessage).Content.Text
	if dispatcher.closeBackgroundFrameFromNotice(&ai.UserMessage{Content: ai.StringOrBlocks{Text: startText}}) {
		t.Fatal("the start notice stopped the frame")
	}
	if again, _ := component.AnimationFrame(time.Now()); !again {
		t.Fatal("the frame stopped ticking while the call was still running")
	}

	// The finish notice finalizes and releases it. A real handle records its start,
	// so the notice also states how long the call took ("… finished in 1m 30s");
	// the status is then no longer the title's suffix, which must not stop it
	// matching.
	finish := agent.CompletionNotice(agent.ToolCallHandle{ID: "bg-9", ToolName: "bash", OutputPath: "/tmp/x.log",
		StartedAt: time.Now().Add(-90 * time.Second)},
		agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "done"}}})
	event := noticeFor(finish.(*ai.UserMessage).Content.Text)
	event.Agent.Message = finish
	dispatcher.HandleEvent(event)
	if _, ok := dispatcher.pendingTools["c9"]; ok {
		t.Fatal("the finish notice did not release the frame")
	}
	if again, _ := component.AnimationFrame(time.Now()); again {
		t.Fatal("the frame kept ticking after the call finished")
	}
}

// A run's end used to drop the pending map without finalizing, leaving each
// component PARTIAL; a shell frame ticks while its result is partial or absent,
// so an abandoned frame ticked forever (D538 fix for the 7m18s frame). A run end
// must stop every frame it can no longer update, while a displaced call, which
// outlives its run, keeps ticking until its own notice closes it.
func TestAgentEndStopsOrphanedFramesButKeepsABackgroundedOne(t *testing.T) {
	newRendererTestTheme(t)
	transcript := NewTranscriptRenderer(&tui.Container{}, nil, nil, nil, nil)
	dispatcher := NewEventDispatcher(transcript, nil, nil, nil, nil, nil, nil)

	plain := NewToolExecutionComponent("bash", "c1", map[string]any{}, ToolExecutionOptions{},
		WithBuiltInRenderers("bash", nil), nil, "/tmp")
	plain.MarkExecutionStarted()
	dispatcher.pendingTools["c1"] = plain

	displaced := NewToolExecutionComponent("bash", "c2", map[string]any{}, ToolExecutionOptions{},
		WithBuiltInRenderers("bash", nil), nil, "/tmp")
	displaced.MarkExecutionStarted()
	dispatcher.pendingTools["c2"] = displaced
	dispatcher.handleToolExecutionEnd(&coding.SessionEvent{Agent: &agent.AgentEvent{
		Type: agent.ToolExecutionEnd, ToolCallID: "c2", ToolName: "bash",
		Result: agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "moved"}},
			Details: json.RawMessage(`{"backgroundHandle":"bg-2","toolCallId":"c2","toolName":"bash","outputPath":"/tmp/y.log"}`)},
	}})
	if again, _ := displaced.AnimationFrame(time.Now()); !again {
		t.Fatal("the displaced frame was not ticking before the run ended")
	}

	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionAgentEnd})

	if again, _ := plain.AnimationFrame(time.Now()); again {
		t.Fatal("an abandoned frame kept ticking after its run ended")
	}
	if again, _ := displaced.AnimationFrame(time.Now()); !again {
		t.Fatal("a displaced call's frame stopped with the run that started it, though the call is still running")
	}
	if _, ok := dispatcher.pendingTools["c2"]; !ok {
		t.Fatal("the displaced frame is no longer tracked, so live updates and its notice reach nothing")
	}
}
