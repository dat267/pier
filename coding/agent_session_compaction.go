package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/dat267/pier/ai"
)

// Port of the automatic-compaction decision (core/agent-session.ts
// _checkCompaction / _runAutoCompaction): the overflow, recoverable-length, and
// threshold cases with their guard rails.

// overflowRecoveryState is the one-attempt overflow recovery bookkeeping.
type overflowRecoveryState struct {
	attempted bool
}

// CheckCompaction decides whether automatic compaction should run for the given
// assistant message and runs it. It reports whether the caller should continue
// the agent (overflow recovery retries the turn).
func (s *AgentSession) CheckCompaction(ctx context.Context, assistant *ai.AssistantMessage, skipAbortedCheck bool) (bool, error) {
	if assistant == nil {
		return false, nil
	}
	settings, enabled := s.compactionSettings()
	if !enabled {
		return false, nil
	}

	// An aborted message (user cancelled) is skipped unless the caller wants it
	// considered (the pre-prompt check).
	if skipAbortedCheck && assistant.StopReason == ai.StopAborted {
		return false, nil
	}

	model := s.Model()
	contextWindow := int64(0)
	if model != nil {
		contextWindow = model.ContextWindow
	}

	// Overflow from a different model must not trigger compaction for the
	// current one (for example after switching to a larger-context model).
	sameModel := model != nil && string(assistant.Provider) == string(model.Provider) && assistant.Model == model.ID

	// Stale pre-compaction usage or errors must not retrigger compaction right
	// after one finished.
	compactionEntry := s.Sessions.LatestCompaction()
	if compactionEntry != nil && assistant.Timestamp > 0 {
		if assistant.Timestamp <= entryTimestampMS(compactionEntry) {
			return false, nil
		}
	}

	// Cases 1 and 2: context overflow (or a recoverable length stop). Only an
	// assistant that is still part of the projection can recover, and its usage
	// only describes the current context when no edit came after it; an explicit
	// overflow error additionally needs the attempt to be retained, not omitted
	// or compacted away (upstream assistantIsProjected /
	// assistantUsageMatchesProjection / assistantRetainedForExplicitRecovery).
	assistantID := s.entryIDForMessage(assistant)
	assistantIsProjected := assistantID == ""
	for _, entry := range s.Sessions.ProjectedEntries() {
		if assistantID == "" || entry.Source.ID != assistantID {
			continue
		}
		for _, message := range entry.Messages {
			if ai.RoleOf(message) == ai.RoleAssistant {
				assistantIsProjected = true
			}
		}
	}

	hasPostAssistantEdit := false
	compactedAfterAssistant := false
	assistantOmitted := false
	if assistantID != "" {
		branch := s.Sessions.GetBranch("")
		assistantIndex := -1
		for index := range branch {
			if branch[index].ID == assistantID {
				assistantIndex = index
				break
			}
		}
		if assistantIndex >= 0 {
			for _, entry := range branch[assistantIndex+1:] {
				if entry.Type == "compaction" {
					compactedAfterAssistant = true
				}
				if entry.Type == "context_edit" {
					hasPostAssistantEdit = true
					if entry.TargetID == assistantID {
						assistantOmitted = contextEditIsOmission(entry.Replacement)
					}
				}
			}
		}
	}
	assistantRetained := assistantID == "" || (!compactedAfterAssistant && !assistantOmitted)
	assistantUsageMatchesProjection := assistantIsProjected && !hasPostAssistantEdit
	explicitOverflow := assistant.StopReason == ai.StopError && ai.IsContextOverflow(assistant, 0)
	contextOverflow := sameModel &&
		((explicitOverflow && assistantRetained) ||
			(assistantUsageMatchesProjection && ai.IsContextOverflow(assistant, contextWindow)))
	recoverableLength := sameModel && assistantIsProjected && ai.IsRecoverableLength(assistant, maxTokensOf(model))
	if contextOverflow || recoverableLength {
		willRetry := assistant.StopReason != ai.StopStop

		// Case 2: the response completed successfully; compact without retrying
		// (agent.continue cannot continue a completed response).
		if !willRetry {
			return s.RunAutoCompaction(ctx, CompactionOverflow, false)
		}

		s.mu.Lock()
		attempted := s.overflowRecovery.attempted
		s.mu.Unlock()
		if attempted {
			errorMessage := "Context overflow recovery failed after one compact-and-retry attempt. Try reducing context or switching to a larger-context model."
			if !contextOverflow {
				errorMessage = "Truncated response recovery failed after one compact-and-retry attempt."
			}
			s.emit(&SessionEvent{
				Type: SessionCompactionEnd, Reason: CompactionOverflow,
				Aborted: false, ErrorMessage: errorMessage,
			})
			return false, nil
		}

		// Case 1: persist an omission of the failed/truncated response and its tool
		// results from model context, compact, and retry once. They stay in session
		// history but the post-compaction projection must not restore them, or
		// agent.continue refuses with "Cannot continue from message role: assistant".
		s.mu.Lock()
		s.overflowRecovery.attempted = true
		s.mu.Unlock()
		if err := s.omitRecoveryAttempt(assistant); err != nil {
			return false, err
		}
		return s.RunAutoCompaction(ctx, CompactionOverflow, willRetry)
	}

	// Case 3: threshold compaction without retry. Error or zero-usage messages
	// estimate from the last valid response so persistent API errors can still
	// compact and do not reset context accounting. A context edit invalidates
	// provider usage, so those sessions are estimated from the projection.
	contextTokens := int64(0)
	directContextTokens := int64(ai.CalculateContextTokens(assistant.Usage))
	if s.Sessions.HasContextEdits() {
		contextTokens = int64(s.Sessions.ProjectedContextEstimate().Tokens)
	} else if assistant.StopReason == ai.StopError || directContextTokens == 0 {
		messages := s.Agent.State().Messages
		estimate := EstimateContextTokens(messages)
		if estimate.LastUsageIndex >= 0 && compactionEntry != nil && estimate.LastUsageIndex < len(messages) {
			// Only usage-backed estimates need the stale pre-compaction check:
			// kept pre-compaction usage reflects the old, larger context.
			if usageAssistant, ok := messages[estimate.LastUsageIndex].(*ai.AssistantMessage); ok {
				if usageAssistant.Timestamp > 0 && usageAssistant.Timestamp <= entryTimestampMS(compactionEntry) {
					return false, nil
				}
			}
		}
		contextTokens = int64(estimate.Tokens)
	} else {
		contextTokens = directContextTokens
	}

	if ShouldCompact(contextTokens, contextWindow, settings) {
		return s.RunAutoCompaction(ctx, CompactionThreshold, false)
	}
	return false, nil
}

// entryIDForMessage returns the session entry id that persists a message
// (upstream _findPersistedMessageEntryId): the live event-stream mapping first,
// then persisted message provenance, then the projection's positional
// provenance. The fallbacks cover messages a resume installed directly into
// agent state without message_end events.
func (s *AgentSession) entryIDForMessage(message ai.Message) string {
	s.mu.Lock()
	if id := s.entryIDsByMessage[message]; id != "" {
		s.mu.Unlock()
		return id
	}
	s.mu.Unlock()
	if id := s.Sessions.MessageEntryID(message); id != "" {
		s.cacheEntryIDForMessage(message, id)
		return id
	}
	messageIndex := -1
	for index, candidate := range s.Agent.State().Messages {
		if candidate == message {
			messageIndex = index
			break
		}
	}
	if messageIndex < 0 {
		return ""
	}
	projectedIndex := 0
	for _, entry := range s.Sessions.ProjectedEntries() {
		nextIndex := projectedIndex + len(entry.Messages)
		if messageIndex < nextIndex {
			s.cacheEntryIDForMessage(message, entry.Source.ID)
			return entry.Source.ID
		}
		projectedIndex = nextIndex
	}
	return ""
}

// cacheEntryIDForMessage records a provenance-resolved mapping (upstream
// _findPersistedMessageEntryId memoizes the projection fallback).
func (s *AgentSession) cacheEntryIDForMessage(message ai.Message, id string) {
	s.mu.Lock()
	if s.entryIDsByMessage == nil {
		s.entryIDsByMessage = map[ai.Message]string{}
	}
	s.entryIDsByMessage[message] = id
	s.mu.Unlock()
}

// omitRecoveryAttempt persists a context_edit that excludes the failed response
// and its tool results from model context (upstream _omitRecoveryAttempt). The
// results are found in the projected context rather than taken from the caller,
// so an attempt that a resumed session persisted with its results is omitted as
// a pair; an unpaired result makes providers reject every later request
// (upstream v1.4.0 #11227).
func (s *AgentSession) omitRecoveryAttempt(assistant *ai.AssistantMessage) error {
	toolResults := s.omissionToolResults(assistant)
	assistantID := s.entryIDForMessage(assistant)
	ids := make([]string, 0, len(toolResults)+1)
	ids = append(ids, assistantID)
	for _, result := range toolResults {
		ids = append(ids, s.entryIDForMessage(result))
	}

	// A projected target with no source entry cannot be omitted; a target that
	// is not in agent state is simply skipped, like upstream.
	agentMessages := s.Agent.State().Messages
	for index, id := range ids {
		if id != "" {
			continue
		}
		target := ai.Message(assistant)
		if index > 0 {
			target = toolResults[index-1]
		}
		if slices.Contains(agentMessages, target) {
			return fmt.Errorf("Cannot persist recovery omission because a projected message has no source entry")
		}
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		editID, err := s.Sessions.AppendContextEdit(id, json.RawMessage("null"))
		if err != nil {
			return err
		}
		if entry := s.Sessions.GetEntry(editID); entry != nil {
			s.emit(&SessionEvent{Type: SessionEntryAppended, Entry: entry})
		}
	}
	// Refresh immediately (upstream _omitRecoveryAttempt calls
	// _refreshFinalizedContext): the omission must reach live agent state even
	// if the follow-up summarization fails.
	sessionContext := s.Sessions.Projection()
	s.Agent.SetMessages(sessionContext.Messages)
	s.refreshFinalizedContext(sessionContext)
	return nil
}

// omissionToolResults finds the tool results answering the message's tool calls
// in agent state: the messages after it up to the next assistant message, matched
// by call id. It mirrors upstream _omitRecoveryAttempt's search, including
// matching a projected copy through the persisted entry id.
func (s *AgentSession) omissionToolResults(assistant *ai.AssistantMessage) []ai.Message {
	callIDs := map[string]struct{}{}
	for _, block := range assistant.Content {
		if call, ok := block.(ai.ToolCall); ok && call.ID != "" {
			callIDs[call.ID] = struct{}{}
		}
	}
	if len(callIDs) == 0 {
		return nil
	}
	assistantID := s.entryIDForMessage(assistant)
	messages := s.Agent.State().Messages
	index := -1
	s.mu.Lock()
	for position, message := range messages {
		if message == assistant || (assistantID != "" && s.entryIDsByMessage[message] == assistantID) {
			index = position
			break
		}
	}
	s.mu.Unlock()
	if index < 0 {
		return nil
	}
	results := make([]ai.Message, 0, len(callIDs))
	for _, message := range messages[index+1:] {
		if ai.RoleOf(message) == ai.RoleAssistant {
			break
		}
		if result, ok := message.(*ai.ToolResultMessage); ok {
			if _, wanted := callIDs[result.ToolCallID]; wanted {
				results = append(results, message)
			}
		}
	}
	return results
}

func maxTokensOf(model *ai.Model) int64 {
	if model == nil {
		return 0
	}
	return model.MaxTokens
}

// compactionSettings resolves the compaction settings and whether automatic
// compaction is enabled: the settings manager decides when one is wired
// (upstream's setAutoCompactionEnabled writes there), otherwise the session
// toggle gates the session-level settings.
func (s *AgentSession) compactionSettings() (CompactionSettings, bool) {
	if s.control.Settings != nil {
		resolved, err := s.control.Settings.GetCompactionSettings(s.Model())
		if err == nil {
			return CompactionSettings{
				Enabled: resolved.Enabled, ReserveTokens: resolved.ReserveTokens,
				KeepRecentTokens: resolved.KeepRecentTokens,
			}, resolved.Enabled
		}
	}
	settings := s.Settings.Compaction
	if settings.ReserveTokens == 0 && settings.KeepRecentTokens == 0 {
		settings = DefaultCompactionSettings
	}
	return settings, settings.Enabled && s.AutoCompactionEnabled()
}

// RunAutoCompaction executes threshold or overflow compaction. It reports
// whether the post-run loop should continue the agent.
func (s *AgentSession) RunAutoCompaction(ctx context.Context, reason CompactionReason, willRetry bool) (bool, error) {
	model := s.Model()
	if !s.HasModel() || model == nil {
		return false, nil
	}
	settings, _ := s.compactionSettings()
	pathEntries := s.Sessions.GetBranch("")
	preparation := PrepareCompaction(pathEntries, settings)
	if preparation == nil {
		return false, nil
	}

	compactionCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.compactionCancel = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.compactionCancel = nil
		s.mu.Unlock()
		cancel()
		s.resolveIdleWaitIfIdle()
	}()
	// Upstream installs the controller before publishing compaction_start, so
	// listeners can cancel the operation immediately.
	s.emit(&SessionEvent{Type: SessionCompactionStart, Reason: reason})
	if compactionCtx.Err() != nil {
		s.emit(&SessionEvent{Type: SessionCompactionEnd, Reason: reason, Aborted: true})
		return false, compactionCtx.Err()
	}

	options := CompactionOptions{
		Model: model, Ctx: compactionCtx, StreamFn: s.compactionStreamFn(),
		Retry: s.retrySettings(), SessionID: s.Sessions.GetSessionID(),
		ThinkingLevel: s.ThinkingLevel(),
		Callbacks:     s.summarizationRetryCallbacksForCompaction(reason),
	}
	if s.control.ModelRuntime != nil {
		resolution, err := s.control.ModelRuntime.GetAuthForModel(model, &ModelRuntimeAuthOverrides{Ctx: compactionCtx})
		if err == nil && resolution != nil {
			options.APIKey = resolution.Auth.APIKey
			options.Headers = headersToStrings(resolution.Auth.Headers)
			options.Env = resolution.Env
			if resolution.Auth.BaseURL != "" {
				copied := *model
				copied.BaseURL = resolution.Auth.BaseURL
				options.Model = &copied
			}
		}
	}

	result, err := Compact(preparation, options)
	if err != nil {
		s.emit(&SessionEvent{
			Type: SessionCompactionEnd, Reason: reason, Aborted: compactionCtx.Err() != nil,
			ErrorMessage: err.Error(),
		})
		return false, err
	}
	if compactionCtx.Err() != nil {
		s.emit(&SessionEvent{Type: SessionCompactionEnd, Reason: reason, Aborted: true})
		return false, compactionCtx.Err()
	}

	s.Sessions.AppendCompaction(result.Summary, result.FirstKeptEntryID, result.TokensBefore, result.Details, false, result.Usage)
	sessionContext := s.Sessions.Projection()
	s.Agent.SetMessages(sessionContext.Messages)
	// Report the context size after the compaction and refresh the entry mapping
	// to the finalized context (upstream _refreshFinalizedContext plus
	// estimatedTokensAfter).
	result.EstimatedTokensAfter = int64(estimateMessagesTokens(sessionContext.Messages))
	s.refreshFinalizedContext(sessionContext)

	s.emit(&SessionEvent{Type: SessionCompactionEnd, Reason: reason, Result: result, WillRetry: willRetry})
	// An overflow recovery that consumed the failed message retries the turn.
	return willRetry, nil
}

func headersToStrings(headers ai.ProviderHeaders) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range headers {
		if value != nil {
			out[key] = *value
		}
	}
	return out
}

// AbortCompaction cancels an in-flight automatic or manual compaction.
func (s *AgentSession) AbortCompaction() {
	s.mu.Lock()
	cancel := s.compactionCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// AbortBranchSummary cancels an in-flight branch summarization (upstream
// abortBranchSummary).
func (s *AgentSession) AbortBranchSummary() {
	s.control.stateMu.Lock()
	cancel := s.control.branchSummaryCancel
	s.control.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// summarizationRetryCallbacksForCompaction builds the compaction callbacks
// (the attempt-start event carries the compaction reason).
func (s *AgentSession) summarizationRetryCallbacksForCompaction(reason CompactionReason) *SummarizationCallbacks {
	callbacks := s.summarizationRetryCallbacks("compaction")
	callbacks.OnRetryAttemptStart = func() error {
		s.emit(&SessionEvent{Type: SessionSummarizationRetryAttemptStart, Source: "compaction", Reason: reason})
		return nil
	}
	return callbacks
}

// summarizationRetryCallbacks builds the retry callbacks shared by compaction
// and branch-summary summarization (upstream _summarizationRetryCallbacks).
func (s *AgentSession) summarizationRetryCallbacks(source string) *SummarizationCallbacks {
	return &SummarizationCallbacks{
		OnRetryScheduled: func(attempt, maxAttempts int, delayMS int64, errorMessage string) error {
			s.emit(&SessionEvent{
				Type: SessionSummarizationRetryScheduled, Source: source,
				Attempt: attempt, MaxAttempts: maxAttempts, DelayMS: delayMS, ErrorMessage: errorMessage,
			})
			return nil
		},
		OnRetryAttemptStart: func() error {
			s.emit(&SessionEvent{Type: SessionSummarizationRetryAttemptStart, Source: source, Reason: source})
			return nil
		},
		OnRetryFinished: func(success bool, attempt int, finalError string) error {
			s.emit(&SessionEvent{Type: SessionSummarizationRetryFinished, Source: source})
			return nil
		},
	}
}
