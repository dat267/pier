package ai

import (
	"net/http"
	"testing"
)

// TestRetryDelayFallsBackToExponentialOnUnparseableHeaders pins 2bbfcca43
// (#9571): a Retry-After value that parses to a non-finite number must fall
// through to the exponential backoff instead of returning an unusable delay
// (Go's ParseFloat accepts "NaN" and "Infinity" without error, and converting
// those to int64 is not a defined value).
// provider-retry.ts validates the finite JavaScript number before applying its delay cap.
func TestRetryDelayRejectsFiniteHeaderBeyondInt64(t *testing.T) {
	providerErr := &ProviderError{Message: "boom", Headers: http.Header{"Retry-After-Ms": []string{"1e100"}}}
	maxDelay := 60_000
	if _, err := GetRetryDelayMS(providerErr, 0, &maxDelay); err == nil {
		t.Fatal("expected a server-delay cap error for a finite oversized value")
	}
}

// provider-retry.ts uses Number.parseFloat, which accepts a numeric prefix.
func TestRetryDelayParsesNumericPrefixes(t *testing.T) {
	for _, testCase := range []struct {
		header string
		value  string
		want   int64
	}{
		{"Retry-After-Ms", "1250ms", 1250},
		{"Retry-After", "2.5 seconds", 2500},
	} {
		providerErr := &ProviderError{Message: "boom", Headers: http.Header{testCase.header: []string{testCase.value}}}
		got, err := GetRetryDelayMS(providerErr, 0, nil)
		if err != nil || got != testCase.want {
			t.Errorf("GetRetryDelayMS(%q) = %d, %v; want %d, nil", testCase.value, got, err, testCase.want)
		}
	}
}

func TestRetryDelayFallsBackToExponentialOnUnparseableHeaders(t *testing.T) {
	cases := []struct {
		name   string
		header string
		value  string
	}{
		{"retry-after-ms NaN", "Retry-After-Ms", "NaN"},
		{"retry-after-ms Infinity", "Retry-After-Ms", "Infinity"},
		{"retry-after Infinity", "Retry-After", "Infinity"},
		{"retry-after NaN", "Retry-After", "NaN"},
	}
	for _, testCase := range cases {
		err := &ProviderError{Message: "boom", Headers: http.Header{testCase.header: []string{testCase.value}}}
		delay, delayErr := GetRetryDelayMS(err, 0, nil)
		if delayErr != nil {
			t.Fatalf("%s: %v", testCase.name, delayErr)
		}
		// retryIndex 0: min(0.5 * 2^0, 8) * 1000 * (1 - rand*0.25) in [375, 500].
		if delay < 375 || delay > 500 {
			t.Fatalf("%s: delay = %d ms, want the 375-500 ms exponential band", testCase.name, delay)
		}
	}
}
