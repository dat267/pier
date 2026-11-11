package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), utils/retry.ts `isRetryableAssistantError`,
// with every expectation produced by running it. The corpus holds each needle
// the reference's retryable and non-retryable pattern lists document, the texts
// that must stay non-retryable because they are subscription, quota or billing
// exhaustion rather than a transient throttle, the status-code boundary cases
// the reference writes with a lookbehind (the port emulates it with consuming
// character classes, so `429x`, `x429`, `429123` and `1500` must stay
// non-retryable), and a deterministic token soup around the same pieces.
//
// Regenerate: node --experimental-strip-types testdata/retry-classify/generate.mjs ../pi > testdata/retry-classify/golden.json
func TestRetryClassificationMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/retry-classify/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ErrorMessage string `json:"errorMessage"`
		Want         bool   `json:"want"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 400 {
		t.Fatalf("fixture has %d cases, expected the full corpus", len(cases))
	}
	for _, testCase := range cases {
		text := testCase.ErrorMessage
		message := &AssistantMessage{Provider: "openai", Model: "m", StopReason: StopError, ErrorMessage: &text}
		if got := IsRetryableAssistantError(message); got != testCase.Want {
			t.Errorf("%q = %v, want %v", text, got, testCase.Want)
		}
	}
	// A message without an error text is never retryable, whatever its stop reason.
	for _, stopReason := range []string{StopStop, StopLength, StopAborted} {
		if IsRetryableAssistantError(&AssistantMessage{StopReason: stopReason}) {
			t.Errorf("stop reason %q without an error message is retryable", stopReason)
		}
	}
}
