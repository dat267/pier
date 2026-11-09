package ai

import "testing"

// pi v1.5.0 (f9a26365) packages/ai/src/utils/retry.ts retryDelayMs:
//
//	const delay = policy.baseDelayMs * 2 ** Math.max(0, attempt - 1);
//	const safeDelay = Number.isSafeInteger(delay) ? delay : Number.MAX_SAFE_INTEGER;
//	return Math.min(safeDelay, policy.maxAgentDelayMs ?? DEFAULT_MAX_AGENT_RETRY_DELAY_MS);
//
// `??` keeps a configured zero: `maxAgentDelayMs: 0` makes every retry delay
// zero, and `setTimeout(0)` fires immediately. The port substituted the 60 s
// default for a zero cap, so settings that asked for no backoff waited a
// minute between retries.
func TestRetryDelayMSMatchesReference(t *testing.T) {
	for _, test := range []struct {
		policy  RetryPolicy
		attempt int
		want    int64
	}{
		{RetryPolicy{BaseDelayMS: 2000}, 1, 2000},
		{RetryPolicy{BaseDelayMS: 2000}, 2, 4000},
		{RetryPolicy{BaseDelayMS: 2000}, 3, 8000},
		{RetryPolicy{BaseDelayMS: 1000, MaxAgentDelayMS: 250, MaxAgentDelayMSSet: true}, 1, 250},
		{RetryPolicy{BaseDelayMS: 1000, MaxAgentDelayMS: 250, MaxAgentDelayMSSet: true}, 3, 250},
		{RetryPolicy{BaseDelayMS: 1000, MaxAgentDelayMS: 0, MaxAgentDelayMSSet: true}, 1, 0},
		{RetryPolicy{BaseDelayMS: 0, MaxAgentDelayMS: 250, MaxAgentDelayMSSet: true}, 1, 0},
		{RetryPolicy{BaseDelayMS: 1000, MaxAgentDelayMS: -5, MaxAgentDelayMSSet: true}, 1, -5},
		{RetryPolicy{BaseDelayMS: 1000}, 0, 1000},
		{RetryPolicy{BaseDelayMS: 1000}, -3, 1000},
		{RetryPolicy{BaseDelayMS: 1000}, 60, 60000},
		{RetryPolicy{BaseDelayMS: 1000}, 1000, 60000},
		{RetryPolicy{BaseDelayMS: 1000, MaxAgentDelayMS: 1e15, MaxAgentDelayMSSet: true}, 60, 1e15},
		{RetryPolicy{BaseDelayMS: 1000, MaxAgentDelayMS: 1e15, MaxAgentDelayMSSet: true}, 100, 1e15},
	} {
		if got := RetryDelayMS(test.policy, test.attempt); got != test.want {
			t.Errorf("RetryDelayMS(%+v, %d) = %d, want %d", test.policy, test.attempt, got, test.want)
		}
	}
}

// An unset cap keeps the 60 s default, so the flag is what distinguishes the
// two states rather than the value.
func TestRetryDelayMSDefaultsAnUnsetCap(t *testing.T) {
	if got := RetryDelayMS(RetryPolicy{BaseDelayMS: 1000}, 20); got != DefaultMaxAgentRetryDelayMS {
		t.Errorf("got %d, want %d", got, DefaultMaxAgentRetryDelayMS)
	}
	// A delay beyond JavaScript's safe-integer range clamps to
	// Number.MAX_SAFE_INTEGER before the cap applies, like the reference.
	const maxSafeInteger = 1<<53 - 1
	if got := RetryDelayMS(RetryPolicy{BaseDelayMS: 1000, MaxAgentDelayMS: 1 << 60, MaxAgentDelayMSSet: true}, 200); got != maxSafeInteger {
		t.Errorf("got %d, want %d", got, maxSafeInteger)
	}
}
