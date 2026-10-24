package ai

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

// Reference: pi v1.4.0 packages/ai/src/utils/retry.ts retryAssistantCall
// uses optional chaining for every callback, including onRetryFinished.
func TestAssistantRetryAllowsPartialCallbacks(t *testing.T) {
	for _, reason := range []StopReason{StopStop, StopAborted, StopError} {
		t.Run(string(reason), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				message := "server overloaded"
				produce := func() (*AssistantMessage, error) {
					calls++
					if calls == 1 {
						return &AssistantMessage{StopReason: StopError, ErrorMessage: &message}, nil
					}
					return &AssistantMessage{StopReason: reason}, nil
				}
				response, err := RetryAssistantCall(produce, &RetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMS: 1}, context.Background(), &RetryCallbacks{OnRetryScheduled: func(int, int, int64, string) error { return nil }})
				if err != nil || response.StopReason != reason || calls != 2 {
					t.Fatalf("response=%+v calls=%d err=%v", response, calls, err)
				}
			})
		})
	}
}

// Reference: retry.ts awaits callback promises. A rejected callback aborts the
// operation rather than being swallowed and allowing another model request.
func TestAssistantRetryPropagatesCallbackErrors(t *testing.T) {
	for _, phase := range []string{"scheduled", "attempt-start", "finished", "cancelled"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				want := errors.New("callback failure")
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				callbacks := &RetryCallbacks{}
				switch phase {
				case "scheduled":
					callbacks.OnRetryScheduled = func(int, int, int64, string) error { return want }
				case "attempt-start":
					callbacks.OnRetryAttemptStart = func() error { return want }
				case "finished":
					callbacks.OnRetryFinished = func(bool, int, string) error { return want }
				case "cancelled":
					callbacks.OnRetryScheduled = func(int, int, int64, string) error { cancel(); return nil }
					callbacks.OnRetryFinished = func(bool, int, string) error { return want }
				}
				calls := 0
				message := "server overloaded"
				_, err := RetryAssistantCall(func() (*AssistantMessage, error) {
					calls++
					if calls == 1 {
						return &AssistantMessage{StopReason: StopError, ErrorMessage: &message}, nil
					}
					return &AssistantMessage{StopReason: StopStop}, nil
				}, &RetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMS: 1}, ctx, callbacks)
				if !errors.Is(err, want) {
					t.Fatalf("error=%v want=%v", err, want)
				}
				if phase != "finished" && calls != 1 {
					t.Fatalf("callback failure allowed %d model requests", calls)
				}
			})
		})
	}
}
