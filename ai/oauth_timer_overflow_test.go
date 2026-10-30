package ai

import (
	"context"
	"testing"
	"time"
)

// pi v1.4.0 device-code.ts passes the server interval to abortableSleep, which
// uses setTimeout and therefore clamps a delay above 2^31-1 to 1ms.
func TestOAuthDeviceExpirationBeyondDurationRangeDoesNotOverflow(t *testing.T) {
	interval := 1
	expires := 10_000_000_000
	value, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{
		IntervalSeconds:  &interval,
		ExpiresInSeconds: &expires,
		Poll: func() (OAuthDeviceCodePollResult[string], error) {
			return OAuthDeviceCodePollResult[string]{Status: "complete", Value: "done"}, nil
		},
	})
	if err != nil {
		t.Fatalf("future expiration overflowed: %v", err)
	}
	if value != "done" {
		t.Fatalf("value=%q, want complete result", value)
	}
}

func TestOAuthDevicePollingIntervalOverflowMatchesJavaScriptTimeout(t *testing.T) {
	interval := 2_147_484 // seconds produce a delay above the signed 32-bit ms range
	calls := 0
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	value, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{
		IntervalSeconds: &interval,
		Ctx:             ctx,
		Poll: func() (OAuthDeviceCodePollResult[string], error) {
			calls++
			if calls == 1 {
				return OAuthDeviceCodePollResult[string]{Status: "pending"}, nil
			}
			return OAuthDeviceCodePollResult[string]{Status: "complete", Value: "done"}, nil
		},
	})
	if err != nil {
		t.Fatalf("device poll failed: %v", err)
	}
	if value != "done" || calls != 2 {
		t.Fatalf("value=%q polls=%d; want completion on the second poll", value, calls)
	}
}
