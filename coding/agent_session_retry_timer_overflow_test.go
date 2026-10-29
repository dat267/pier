package coding

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 coding-agent/src/utils/sleep.ts uses setTimeout(ms), whose runtime
// clamps values above 2^31-1 to 1ms instead of sleeping for days.
func TestPrepareRetryDelayOverflowMatchesJavaScriptTimeout(t *testing.T) {
	policy := &ai.RetryPolicy{
		Enabled: true, MaxRetries: 1, BaseDelayMS: 1 << 31, MaxAgentDelayMS: 1 << 31,
	}
	session := retrySession(t, policy, erroringStreamFn(0, ""))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	message := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		StopReason: ai.StopError, ErrorMessage: new("overloaded_error"),
	}
	retry, err := session.PrepareRetry(ctx, message)
	if err != nil {
		t.Fatalf("prepare retry: %v", err)
	}
	if !retry {
		t.Fatal("retry did not run after the overflowing delay")
	}
}
