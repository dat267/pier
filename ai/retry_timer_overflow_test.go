package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// pi v1.4.0 provider-retry.ts abortableSleep uses setTimeout(ms), whose runtime
// clamps a server delay above 2^31-1 to 1ms when the optional cap is disabled.
func TestProviderRetryDelayOverflowMatchesJavaScriptTimeout(t *testing.T) {
	maxRetries := 1
	noDelayCap := 0
	attempts := 0
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	response, err := RetryProviderRequest(ctx, func() (*http.Response, error) {
		attempts++
		if attempts == 1 {
			headers := make(http.Header)
			headers.Set("retry-after-ms", "2147483648")
			return nil, &ProviderError{Status: http.StatusTooManyRequests, Headers: headers, Message: "rate limited"}
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}, &ProviderRetryOptions{MaxRetries: &maxRetries, MaxRetryDelayMS: &noDelayCap})
	if err != nil {
		t.Fatalf("retry failed after overflowing server delay: %v", err)
	}
	defer response.Body.Close()
	if attempts != 2 {
		t.Fatalf("request attempts = %d, want retry after the first attempt", attempts)
	}
}

// pi v1.4.0 utils/retry.ts uses setTimeout for assistant retry backoff too.
func TestAssistantRetryDelayOverflowMatchesJavaScriptTimeout(t *testing.T) {
	message := "server overloaded"
	calls := 0
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	policy := &RetryPolicy{
		Enabled: true, MaxRetries: 1, BaseDelayMS: 1 << 31, MaxAgentDelayMS: 1 << 31,
	}
	response, err := RetryAssistantCall(func() (*AssistantMessage, error) {
		calls++
		if calls == 1 {
			return &AssistantMessage{StopReason: StopError, ErrorMessage: &message}, nil
		}
		return &AssistantMessage{StopReason: StopStop}, nil
	}, policy, ctx, nil)
	if err != nil {
		t.Fatalf("assistant retry failed: %v", err)
	}
	if response.StopReason != StopStop || calls != 2 {
		t.Fatalf("response=%+v calls=%d; want success on the retry", response, calls)
	}
}
