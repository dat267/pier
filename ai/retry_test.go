package ai

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
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
		{"Retry-After-Ms", "\u00a01250ms", 1250},
		{"Retry-After", "2.5 seconds", 2500},
	} {
		providerErr := &ProviderError{Message: "boom", Headers: http.Header{testCase.header: []string{testCase.value}}}
		got, err := GetRetryDelayMS(providerErr, 0, nil)
		if err != nil || got != testCase.want {
			t.Errorf("GetRetryDelayMS(%q) = %d, %v; want %d, nil", testCase.value, got, err, testCase.want)
		}
	}
}

// provider-retry.ts retries APIConnectionError instances with no HTTP status.
func TestRetryableNetworkErrorsMatchSDKConnectionErrors(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "https://api.example/v1", Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}}
	if !isRetryableProviderError(err) {
		t.Fatal("network connection error is not retryable")
	}
	if isRetryableProviderError(errors.New("invalid request")) {
		t.Fatal("ordinary local error should not be retried")
	}
	badURL := &url.Error{Op: "Post", URL: "bad:", Err: errors.New("unsupported protocol scheme")}
	if isRetryableProviderError(badURL) {
		t.Fatal("invalid request URL should not be retried")
	}
}

// RetryProviderRequest retries network errors like the SDK's APIConnectionError.
func TestRetryProviderRequestRetriesConnectionErrors(t *testing.T) {
	previousSleep := providerRetrySleep
	providerRetrySleep = func(context.Context, int64) error { return nil }
	defer func() { providerRetrySleep = previousSleep }()

	maxRetries := 1
	attempts := 0
	response, err := RetryProviderRequest(context.Background(), func() (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, &url.Error{Op: "Post", URL: "https://api.example/v1", Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}}
		}
		return &http.Response{StatusCode: http.StatusOK}, nil
	}, &ProviderRetryOptions{MaxRetries: &maxRetries})
	if err != nil || response == nil || response.StatusCode != http.StatusOK || attempts != 2 {
		t.Fatalf("response = %v, err = %v, attempts = %d; want one retry and success", response, err, attempts)
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

// Upstream interpolates Math.ceil(delayMs/1000) with String(number), so a large
// delay is not rendered in exponential notation.
func TestServerRetryDelayMessageUsesJSNumber(t *testing.T) {
	max := 1000
	_, err := validateServerRetryDelayFloat(1e9, &max, "boom")
	if err == nil || !strings.Contains(err.Error(), "Server requested 1000000s retry delay") {
		t.Fatalf("err = %v", err)
	}
}
