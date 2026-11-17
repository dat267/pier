package ai

import (
	"testing"
	"time"
)

// A zero max retry delay disables the cap rather than forbidding every delay
// (stream.ts validates `maxDelay > 0 && delay > maxDelay`).
func TestCodexRetryDelayZeroMaxDisablesTheCap(t *testing.T) {
	zero := 0
	if _, err := ValidateCodexRetryDelay(time.Second, &OpenAICodexResponsesOptions{MaxRetryDelayMs: &zero}); err != nil {
		t.Fatalf("zero max delay rejected a delay: %v", err)
	}
	limit := 1000
	if _, err := ValidateCodexRetryDelay(2*time.Second, &OpenAICodexResponsesOptions{MaxRetryDelayMs: &limit}); err == nil {
		t.Fatal("a delay above the cap was accepted")
	}
	if got, err := ValidateCodexRetryDelay(time.Second, &OpenAICodexResponsesOptions{MaxRetryDelayMs: &limit}); err != nil || got != time.Second {
		t.Fatalf("delay at the cap = %v, %v; want accepted", got, err)
	}
}
