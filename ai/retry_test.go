package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
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

// Status codes are only status codes when they stand alone. Upstream v1.4.0
// 6e491cfa9 (issue #11057): digits inside a request id or a byte count used to
// match "503"/"524" and made a permanent failure retryable.
func TestRetryableStatusCodesMatchOnlyStandaloneNumbers(t *testing.T) {
	retryable := []string{
		`{"error":{"message":"Internal server error"},"status":500}`,
		`upstream returned 503`,
		`Error: 429 Too Many Requests`,
		`503`,
		`got 524 from the gateway`,
		`code=502, message=bad gateway`,
	}
	for _, message := range retryable {
		if !IsRetryableAssistantError(errorAssistant(message)) {
			t.Fatalf("%q should be retryable", message)
		}
	}
	notRetryable := []string{
		`401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"},"request_id":"req_011CU503xyz"}`,
		`400 image exceeds 5 MB maximum: 5316784 bytes > 5242880 bytes`,
		`400 model gpt-5.1 refuses the request`,
	}
	for _, message := range notRetryable {
		if IsRetryableAssistantError(errorAssistant(message)) {
			t.Fatalf("%q must not be retryable: the digits are not a status code", message)
		}
	}
}

// errorAssistant wraps a provider error message in a failed assistant message.
func errorAssistant(message string) *AssistantMessage {
	text := message
	return &AssistantMessage{StopReason: StopError, ErrorMessage: &text}
}

// Gateway and transport wording relayed by proxies is transient. Upstream
// v1.4.0: 29057ece3 (Codex CLI mid-stream disconnect), 698a2d23b (gateway
// upstream failures sent as response.failed codes, #10920) and 0fbdbd3f8
// (node-fetch system errors from the Vertex OAuth refresh, #10996).
func TestRetryableProviderErrorsMatchRelayedGatewayWording(t *testing.T) {
	retryable := []string{
		// 29057ece3
		"stream disconnected before completion: stream closed before response.completed",
		// 698a2d23b
		"upstream_error: Upstream request failed",
		"upstream_stream_read_error: read tcp 10.0.0.1:443: connection reset by peer",
		"stream was interrupted",
		"Upstream HTTP/2 stream failed",
		// 0fbdbd3f8
		"request to https://oauth2.googleapis.com/token failed, reason:",
		"request to https://oauth2.googleapis.com/token failed, reason: connect ECONNREFUSED 127.0.0.1:9",
	}
	for _, message := range retryable {
		if !IsRetryableAssistantError(errorAssistant(message)) {
			t.Fatalf("%q should be retryable", message)
		}
	}
	// A request that merely failed is not a node-fetch transport failure.
	for _, message := range []string{
		"400 request to the model failed validation",
		"404 upstream request not found",
	} {
		if IsRetryableAssistantError(errorAssistant(message)) {
			t.Fatalf("%q must not be retryable", message)
		}
	}
}

// A gateway can reject a request without a status or a retry hint, relaying
// "The request was rejected." as an Anthropic api_error event. Nothing in that
// wording says the request is malformed, so it is retried like the other
// retryable phrasings; a persistent rejection exhausts the attempt budget.
// When the rejection does carry a 4xx, the transport classifier still declines
// it, because a stated status is authoritative.
func TestRejectedRequestIsRetryableWithoutAStatus(t *testing.T) {
	for _, message := range []string{
		"The request was rejected.",
		"Error: The request was rejected.",
		"The request has been rejected",
	} {
		if !IsRetryableAssistantError(errorAssistant(message)) {
			t.Fatalf("%q should be retryable", message)
		}
	}
	// A limit that happens to mention rejection is still a limit.
	if IsRetryableAssistantError(errorAssistant("insufficient_quota: the request was rejected")) {
		t.Fatal("a quota limit must not become retryable")
	}
	// A stated 4xx is authoritative and is not retried.
	for _, status := range []int{400, 401, 403, 404} {
		if isRetryableProviderError(&ProviderError{Status: status, Message: "The request was rejected."}) {
			t.Fatalf("status %d must not be retryable", status)
		}
	}
}

// A ChatGPT Business workspace limit is set by the workspace admin, so retrying
// it cannot succeed. Upstream v1.4.0 cedefc532 lists it with the subscription
// limits instead of leaving it to the retry policy.
func TestChatGPTWorkspaceLimitIsNotRetryable(t *testing.T) {
	// A limit that arrives with an otherwise retryable status must still be
	// treated as a limit, which is the ordering upstream relies on.
	message := errorAssistant("429 subscription_sharing_workplace_individual_usage_limit_exceeded usage limit reached")
	if IsRetryableAssistantError(message) {
		t.Fatal("a workspace usage limit must not be retried")
	}
	// The shared subscription limit stays non-retryable as before.
	if IsRetryableAssistantError(errorAssistant("429 subscription_sharing_usage_limit_exceeded rate limit")) {
		t.Fatal("a shared subscription limit must not be retried")
	}
}

// Zhipu/Z.ai (open.bigmodel.cn) sends its plan usage limit as HTTP 429 with
// error code 1308; it resets after hours, so a retry cannot succeed (pi v1.5.0
// utils/retry.ts).
func TestZhipuPlanLimitIsNotRetryable(t *testing.T) {
	message := errorAssistant(`429 {"error":{"code":1308,"message":"The usage limit for this plan has been reached."}}`)
	if IsRetryableAssistantError(message) {
		t.Fatal("a Zhipu plan usage limit must not be retried")
	}
	// The code must stand alone: another number containing 1308 is not the limit.
	if !IsRetryableAssistantError(errorAssistant(`500 {"error":{"code":13081,"message":"upstream server error"}}`)) {
		t.Fatal("a longer code must not match the 1308 pattern")
	}
}

// pi v1.4.0 utils/retry.ts builds JavaScript regexes without /s, so `.` does
// not match CR, U+2028 or U+2029 (Go's `.` only excludes LF).
func TestRetryableErrorPatternUsesJavaScriptDot(t *testing.T) {
	for _, text := range []string{"rate\rlimit", "rate\u2028limit", "rate\u2029limit", "rate\nlimit"} {
		message := &AssistantMessage{StopReason: StopError, ErrorMessage: &text}
		if IsRetryableAssistantError(message) {
			t.Errorf("%q must not be retryable", text)
		}
	}
	message := &AssistantMessage{StopReason: StopError, ErrorMessage: new("rate limit")}
	if !IsRetryableAssistantError(message) {
		t.Fatal("ASCII rate limit must stay retryable")
	}
}

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

// A transport that reports only a Node socket error code is retryable:
// "read ECONNRESET", "write EPIPE", "connect ETIMEDOUT 1.2.3.4:443"
// (utils/retry.ts, v1.6.0, #12128). ETIMEDOUT has no space, so the "timed out"
// phrasing cannot match it.
func TestRetryableProviderErrorMatchesBareSocketCodes(t *testing.T) {
	retryable := func(message string) bool {
		return IsRetryableAssistantError(&AssistantMessage{StopReason: StopError, ErrorMessage: &message})
	}
	for _, message := range []string{
		"read ECONNRESET",
		"write EPIPE",
		"connect ETIMEDOUT 140.82.113.3:443",
		"connect ECONNREFUSED 127.0.0.1:443",
		"read ECONNABORTED",
	} {
		if !retryable(message) {
			t.Errorf("%q is not retryable", message)
		}
	}
	// The code must stand alone: a word that merely contains it is not a code.
	for _, message := range []string{"ECONNRESETTING the pool", "not_ECONNRESET"} {
		if retryable(message) {
			t.Errorf("%q must not be retryable", message)
		}
	}
}

// A gateway mid-stream drop relayed as an Anthropic-style `api_error` event is
// retryable: {"type":"error","error":{"type":"api_error","message":"Connection
// closed."}} (retry.ts, v1.6.0, #12064).
func TestRetryableProviderErrorMatchesConnectionClosed(t *testing.T) {
	message := "Connection closed."
	if !IsRetryableAssistantError(&AssistantMessage{StopReason: StopError, ErrorMessage: &message}) {
		t.Fatalf("%q is not retryable", message)
	}
}
