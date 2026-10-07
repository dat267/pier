package ai

import "testing"

// These all mean the same thing: the socket died mid-request. The classifier
// matched provider-shaped text and several fetch/websocket phrasings, but none of
// the OS-level abort strings, so a call that hit one was not retried at all. The
// reported case is Termux/Windows "software caused connection abort"
// (ECONNABORTED) — upstream does not retry it either, so covering it is a
// deliberate divergence (D171).
func TestTransportAbortsAreRetryable(t *testing.T) {
	messages := []string{
		`Post "https://hyper.charm.land/v1/chat/completions": read tcp 192.168.1.12:45272->142.250.197.147:443: read: software caused connection abort`,
		"read tcp 10.0.0.2:443: read: connection reset by peer",
		"write tcp 10.0.0.2:443: write: broken pipe",
		"unexpected EOF",
	}
	for _, message := range messages {
		message := message
		response := &AssistantMessage{StopReason: StopError, ErrorMessage: &message}
		if !IsRetryableAssistantError(response) {
			t.Errorf("a dead socket was not retried: %q", message)
		}
	}
}

// The account/quota guard is checked first and must keep winning, even when the
// message also carries transport text.
func TestTransportPatternsDoNotOverrideTheLimitGuard(t *testing.T) {
	message := "connection reset by peer while reading insufficient_quota response"
	response := &AssistantMessage{StopReason: StopError, ErrorMessage: &message}
	if IsRetryableAssistantError(response) {
		t.Error("a usage-limit error was retried because its text mentioned a reset")
	}
}

// A provider that reports its selected model is at capacity is retrying a
// transient load condition, not returning a caller error (3874b3e98).
func TestCapacityErrorsAreRetryable(t *testing.T) {
	message := "Selected model is at capacity. Please try a different model."
	response := &AssistantMessage{StopReason: StopError, ErrorMessage: &message}
	if !IsRetryableAssistantError(response) {
		t.Error("a model-at-capacity error was not retried")
	}
}

// Sign in with ChatGPT reports temporary usage/user-data unavailability
// mid-stream; upstream retries it (retry.ts RETRYABLE_PROVIDER_ERROR_PATTERN).
func TestChatGPTSubscriptionUnavailableIsRetryable(t *testing.T) {
	for _, message := range []string{
		"subscription_sharing_usage_unavailable",
		"subscription_sharing_user_unavailable",
	} {
		message := message
		response := &AssistantMessage{StopReason: StopError, ErrorMessage: &message}
		if !IsRetryableAssistantError(response) {
			t.Errorf("a ChatGPT subscription unavailability error was not retried: %q", message)
		}
	}
}

// The ChatGPT subscription's shared usage limit resets after hours, so it is a
// non-retryable account limit (retry.ts NON_RETRYABLE_PROVIDER_LIMIT_ERROR_PATTERN).
// It must not be retried even when the same message also carries 429 text.
func TestChatGPTSubscriptionLimitIsNotRetryable(t *testing.T) {
	message := "429 subscription_sharing_usage_limit_exceeded rate limit"
	response := &AssistantMessage{StopReason: StopError, ErrorMessage: &message}
	if IsRetryableAssistantError(response) {
		t.Error("a ChatGPT subscription usage limit was retried")
	}
}
