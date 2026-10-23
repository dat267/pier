package coding

import (
	"context"
	"time"

	"github.com/dat267/pier/ai"
)

// Port of the auto-retry section of core/agent-session.ts and the post-run
// continuation loop of _handlePostAgentRun.

// IsRetryableError reports whether an assistant error is worth retrying.
// Context overflow is handled by compaction instead.
func (s *AgentSession) IsRetryableError(message *ai.AssistantMessage) bool {
	if message == nil {
		return false
	}
	model := s.Model()
	contextWindow := int64(0)
	if model != nil {
		contextWindow = model.ContextWindow
	}
	if ai.IsContextOverflow(message, contextWindow) {
		return false
	}
	return ai.IsRetryableAssistantError(message)
}

// retrySettings resolves the retry policy from settings or the session config.
func (s *AgentSession) retrySettings() *ai.RetryPolicy {
	if s.control.Settings != nil {
		resolved := s.control.Settings.GetRetrySettings()
		return &ai.RetryPolicy{
			Enabled: resolved.Enabled, MaxRetries: resolved.MaxRetries,
			BaseDelayMS: int(resolved.BaseDelayMS), MaxAgentDelayMS: resolved.MaxAgentDelayMS,
		}
	}
	return s.Settings.Retry
}

// PrepareRetry prepares a retryable error for continuation with exponential
// backoff. It reports whether the caller should continue the agent.
func (s *AgentSession) PrepareRetry(ctx context.Context, message *ai.AssistantMessage) (bool, error) {
	settings := s.retrySettings()
	if settings == nil || !settings.Enabled {
		return false, nil
	}

	s.mu.Lock()
	s.retryAttempt++
	attempt := s.retryAttempt
	s.mu.Unlock()

	if attempt > settings.MaxRetries {
		// Preserve the completed attempt count so post-run handling can emit the
		// final failure.
		s.mu.Lock()
		s.retryAttempt--
		s.mu.Unlock()
		return false, nil
	}

	delayMS := ai.RetryDelayMS(*settings, attempt)
	errorMessage := "Unknown error"
	if message.ErrorMessage != nil && *message.ErrorMessage != "" {
		errorMessage = *message.ErrorMessage
	}
	s.emit(&SessionEvent{
		Type: SessionAutoRetryStart, Attempt: attempt, MaxAttempts: settings.MaxRetries,
		DelayMS: delayMS, ErrorMessage: errorMessage,
	})

	// Persist an omission of the failed response and its tool results, so a
	// resumed session does not replay them to the model (upstream
	// _prepareRetry -> _omitRecoveryAttempt). They stay in session history; the
	// omitted message leaves live context with the immediate finalized-context
	// refresh inside omitRecoveryAttempt.
	if err := s.omitRecoveryAttempt(message); err != nil {
		return false, err
	}

	retryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	s.retryCancel = cancel
	s.retryActive = true
	s.mu.Unlock()

	timer := time.NewTimer(time.Duration(delayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-retryCtx.Done():
		s.finishCancelledRetry()
		return false, nil
	}

	s.mu.Lock()
	s.retryActive = false
	s.retryCancel = nil
	s.mu.Unlock()
	return true, nil
}

// finishCancelledRetry mirrors upstream _finishCancelledRetry. It also handles
// cancellation after the timer fires but before the next low-level run starts.
func (s *AgentSession) finishCancelledRetry() {
	s.mu.Lock()
	s.retryActive = false
	s.retryCancel = nil
	attempt := s.retryAttempt
	s.retryAttempt = 0
	s.mu.Unlock()
	if attempt > 0 {
		s.emit(&SessionEvent{Type: SessionAutoRetryEnd, Success: false, Attempt: attempt, ErrorMessage: "Retry cancelled"})
	}
}

// AbortRetry cancels an in-flight retry sleep.
func (s *AgentSession) AbortRetry() {
	s.mu.Lock()
	cancel := s.retryCancel
	s.retryCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// IsRetrying reports whether an auto-retry is in progress.
func (s *AgentSession) IsRetrying() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retryActive
}

// runPostRunLoop continues the agent for retries and queued continuations
// (port of _handlePostAgentRun).
func (s *AgentSession) runPostRunLoop(ctx context.Context) error {
	for {
		message := s.takeLastAssistantMessage()
		if ctx.Err() != nil {
			s.finishCancelledRetry()
			return nil
		}
		if message == nil {
			return nil
		}

		if s.IsRetryableError(message) {
			retry, err := s.PrepareRetry(ctx, message)
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				s.finishCancelledRetry()
				return nil
			}
			if retry {
				if err := s.Agent.Continue(ctx); err != nil {
					return err
				}
				continue
			}
		}

		if message.StopReason == ai.StopError {
			s.mu.Lock()
			attempt := s.retryAttempt
			s.retryAttempt = 0
			s.mu.Unlock()
			if attempt > 0 {
				finalError := ""
				if message.ErrorMessage != nil {
					finalError = *message.ErrorMessage
				}
				s.emit(&SessionEvent{Type: SessionAutoRetryEnd, Success: false, Attempt: attempt, ErrorMessage: finalError})
			}
		}

		// Automatic compaction (overflow recovery reports continue).
		compacted, err := s.CheckCompaction(ctx, message, true)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		if compacted {
			if err := s.Agent.Continue(ctx); err != nil {
				return err
			}
			continue
		}

		// Messages queued by agent_end listeners need a continuation.
		if s.Agent.HasQueuedMessages() {
			if err := s.Agent.Continue(ctx); err != nil {
				return err
			}
			continue
		}
		return nil
	}
}

// takeLastAssistantMessage pops the tracked assistant message.
func (s *AgentSession) takeLastAssistantMessage() *ai.AssistantMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	message := s.lastAssistantMessage
	s.lastAssistantMessage = nil
	return message
}

// SetRetryEnabled toggles the persisted retry setting.
func (s *AgentSession) SetRetryEnabled(enabled bool) {
	if s.control.Settings != nil {
		s.control.Settings.SetRetryEnabled(enabled)
		return
	}
	s.mu.Lock()
	if s.Settings.Retry == nil {
		s.Settings.Retry = &ai.RetryPolicy{Enabled: enabled}
	} else {
		s.Settings.Retry.Enabled = enabled
	}
	s.mu.Unlock()
}

// RetryEnabled reports the effective retry toggle.
func (s *AgentSession) RetryEnabled() bool {
	settings := s.retrySettings()
	return settings != nil && settings.Enabled
}
