package ai

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// Round 109 tests: context-overflow detection (port of utils/overflow.ts).

func overflowError(message *AssistantMessage, text string) *AssistantMessage {
	copied := *message
	copied.StopReason = StopError
	copied.ErrorMessage = &text
	return &copied
}

func overflowAssistant(provider string, stopReason string) *AssistantMessage {
	return &AssistantMessage{
		API: APIAnthropicMessages, Provider: provider, Model: "m", StopReason: stopReason,
	}
}

func TestIsContextOverflowPatterns(t *testing.T) {
	overflowMessages := []string{
		"prompt is too long: 213462 tokens > 200000 maximum",
		"Prompt too long",           // z.ai
		"Prompt exceeds max length", // z.ai CN endpoint (3dd803d7e)
		`413 {"error":{"type":"request_too_large","message":"Request exceeds the maximum size"}}`,
		"Input is too long for requested model",
		"Your input exceeds the context window of this model",
		"Requested token count exceeds the model's maximum context length of 131072 tokens",
		"Input length (265330) exceeds model's maximum context length (262144).",
		"The input token count (1196265) exceeds the maximum number of tokens allowed (1048575)",
		"This model's maximum prompt length is 131072 but the request contains 537812 tokens",
		"Please reduce the length of the messages or completion",
		"This endpoint's maximum context length is 131072 tokens.",
		"Input length 265330 exceeds the maximum allowed input length of 262144 tokens.",
		"The input (131073 tokens) is longer than the model's context length (131072 tokens).",
		// LiteLLM's pre-call check (pi v1.5.0 utils/overflow.ts).
		"litellm.ContextWindowExceededError: litellm.BadRequestError: Context Window exceeded for given call.",
		"prompt token count of 265330 exceeds the limit of 262144",
		"the request exceeds the available context size, try increasing it",
		"tokens to keep from the initial prompt is greater than the context length",
		"invalid params, context window exceeds limit",
		"Your request exceeded model token limit: 200000 (requested: 213462)",
		"Prompt has 213462 tokens, but the configured context size is 200000 tokens",
		"model_context_window_exceeded",
		"prompt too long; exceeded max context length by 100 tokens",
		"Range of input length should be [1, 200000]",
		"context_length_exceeded",
		"too many tokens",
		"token limit exceeded",
	}
	for _, text := range overflowMessages {
		message := overflowError(overflowAssistant("anthropic", StopError), text)
		if !IsContextOverflow(message, 0) {
			t.Errorf("expected overflow for %q", text)
		}
	}

	// Cerebras' bodyless status text only counts for the cerebras provider.
	bodyless := "400 status code (no body)"
	if !IsContextOverflow(overflowError(overflowAssistant("cerebras", StopError), bodyless), 0) {
		t.Error("cerebras bodyless overflow must be detected")
	}
	if IsContextOverflow(overflowError(overflowAssistant("anthropic", StopError), bodyless), 0) {
		t.Error("bodyless overflow must not apply to other providers")
	}
}

func TestIsContextOverflowNonOverflow(t *testing.T) {
	// Rate limiting and server errors win over the generic overflow patterns.
	nonOverflow := []string{
		"Throttling error: Too many tokens, please wait before trying again.",
		"Service unavailable: token limit exceeded",
		"Rate limit exceeded",
		"Too many requests",
	}
	for _, text := range nonOverflow {
		message := overflowError(overflowAssistant("amazon-bedrock", StopError), text)
		if IsContextOverflow(message, 0) {
			t.Errorf("expected non-overflow for %q", text)
		}
	}
	// Unrelated error text is not an overflow.
	if IsContextOverflow(overflowError(overflowAssistant("anthropic", StopError), "invalid api key"), 0) {
		t.Error("unrelated errors must not be overflow")
	}
	if IsContextOverflow(nil, 0) {
		t.Error("nil is not overflow")
	}
	if IsContextOverflow(overflowError(overflowAssistant("anthropic", StopError), ""), 0) {
		t.Error("empty messages are not overflow")
	}
}

func TestIsContextOverflowSilentAndLengthStop(t *testing.T) {
	// Silent overflow: a success whose input exceeds the window.
	silent := &AssistantMessage{
		API: APIAnthropicMessages, Provider: "zai", Model: "m", StopReason: StopStop,
		Usage: Usage{Input: 200000, CacheRead: 5000, TotalTokens: 205000},
	}
	if !IsContextOverflow(silent, 200000) {
		t.Error("silent overflow must be detected")
	}
	// Without a window the silent case cannot be detected.
	if IsContextOverflow(silent, 0) {
		t.Error("silent overflow needs the context window")
	}
	// Within the window it is not an overflow.
	within := &AssistantMessage{
		API: APIAnthropicMessages, Provider: "zai", Model: "m", StopReason: StopStop,
		Usage: Usage{Input: 100000, TotalTokens: 100000},
	}
	if IsContextOverflow(within, 200000) {
		t.Error("usage within the window is not overflow")
	}

	// Length-stop overflow: zero output with the input filling the window.
	truncated := &AssistantMessage{
		API: APIAnthropicMessages, Provider: "mimo", Model: "m", StopReason: StopLength,
		Usage: Usage{Input: 199000, CacheRead: 1000, Output: 0, TotalTokens: 200000},
	}
	if !IsContextOverflow(truncated, 200000) {
		t.Error("length-stop overflow must be detected")
	}
	// Zero output but plenty of room is not an overflow.
	roomy := &AssistantMessage{
		API: APIAnthropicMessages, Provider: "mimo", Model: "m", StopReason: StopLength,
		Usage: Usage{Input: 1000, Output: 0, TotalTokens: 1000},
	}
	if IsContextOverflow(roomy, 200000) {
		t.Error("length stops with room are not overflow")
	}
	// A length stop with output produced is not case 3.
	withOutput := &AssistantMessage{
		API: APIAnthropicMessages, Provider: "mimo", Model: "m", StopReason: StopLength,
		Usage: Usage{Input: 199000, Output: 50, TotalTokens: 199050},
	}
	if IsContextOverflow(withOutput, 200000) {
		t.Error("length stops with output are handled by recoverable-length logic")
	}
}

// Reference: pi v1.5.0 f9a26365 utils/overflow.ts adds input and cacheRead as
// JavaScript Numbers, so a provider's near-range usage stays a large positive
// value. Go's int64 addition would wrap and miss the overflow.
func TestIsContextOverflowHandlesHugeUsage(t *testing.T) {
	for _, stop := range []string{StopStop, StopLength} {
		message := &AssistantMessage{
			API: APIAnthropicMessages, Provider: "zai", Model: "m", StopReason: stop,
			Usage: Usage{Input: math.MaxInt64, CacheRead: math.MaxInt64},
		}
		if !IsContextOverflow(message, 200000) {
			t.Errorf("stop %q with huge input+cacheRead must be overflow", stop)
		}
	}
}

func TestIsRecoverableLength(t *testing.T) {
	lengthStop := overflowAssistant("anthropic", StopLength)
	lengthStop.Usage.Output = 100
	if !IsRecoverableLength(lengthStop, 4096) {
		t.Error("output below the desired limit is recoverable")
	}
	if IsRecoverableLength(lengthStop, 100) {
		t.Error("output at the limit is not recoverable")
	}
	if IsRecoverableLength(lengthStop, 0) {
		t.Error("without a desired limit there is nothing to recover")
	}
	normal := overflowAssistant("anthropic", StopStop)
	if IsRecoverableLength(normal, 4096) {
		t.Error("non-length stops are not recoverable")
	}
	if IsRecoverableLength(nil, 4096) {
		t.Error("nil is not recoverable")
	}
}

func TestGetOverflowPatterns(t *testing.T) {
	patterns := GetOverflowPatterns()
	if len(patterns) == 0 {
		t.Fatal("patterns must not be empty")
	}
	// The returned slice is a copy.
	patterns[0] = nil
	if GetOverflowPatterns()[0] == nil {
		t.Fatal("patterns must be copied")
	}
	found := false
	for _, pattern := range GetOverflowPatterns() {
		if pattern.MatchString("too many tokens") {
			found = true
		}
	}
	if !found {
		t.Fatal("generic fallback pattern missing")
	}
}

// HTTP 413 is a request-size overflow for every provider, whatever the body
// says. Upstream v1.4.0 73f8de80b: Copilot rejects a body over its byte limit
// with a generic "failed to parse request" body, so the token-count heuristics
// never matched and the raw error reached the user instead of compacting.
func TestIsContextOverflowRequestSizeStatus(t *testing.T) {
	overflowMessages := []string{
		`github-copilot API error (413): {"message":"failed to parse request","code":""}`,
		"custom-provider API error (413): Payload Too Large",
		`413 {"error":{"message":"failed to parse request","code":""}}`,
		"413",
	}
	for _, text := range overflowMessages {
		message := overflowError(overflowAssistant("github-copilot", StopError), text)
		// A byte-size overflow needs no token accounting and no window hint.
		if !IsContextOverflow(message, 1_000_000) {
			t.Errorf("expected overflow for %q", text)
		}
		if !IsContextOverflow(message, 0) {
			t.Errorf("expected overflow without a context window for %q", text)
		}
	}

	// Only a leading status is a status: a 413 inside the body or a longer number
	// that merely starts with 413 is unrelated.
	notOverflow := []string{
		`github-copilot API error (400): {"message":"failed to parse request","code":""}`,
		"custom-provider API error (429): Too many requests",
		`github-copilot API error (400): {"message":"failed to parse request: invalid value 413"}`,
		"custom-provider API error (400): upstream mentioned OpenAI API error (413): invalid input",
		"custom-provider API error (4130): invalid input",
		"4130 failed to parse request",
	}
	for _, text := range notOverflow {
		if IsContextOverflow(overflowError(overflowAssistant("github-copilot", StopError), text), 1_000_000) {
			t.Errorf("expected no overflow for %q", text)
		}
	}

	// The status only counts on a failed response.
	for _, stopReason := range []string{StopAborted, StopStop} {
		message := overflowError(overflowAssistant("github-copilot", StopError), `github-copilot API error (413): failed to parse request`)
		message.StopReason = stopReason
		if IsContextOverflow(message, 1_000_000) {
			t.Errorf("stop reason %q must not be an overflow", stopReason)
		}
	}
	// A bodyless 400 stays Cerebras-only, so a 413 pattern does not widen it.
	bodyless := overflowError(overflowAssistant("opencode-go", StopError), "400 status code (no body)")
	if IsContextOverflow(bodyless, 1_000_000) {
		t.Error("bodyless 400 must stay specific to cerebras")
	}
}

// Reference: pi v1.4.0 utils/overflow.ts regexes use JS whitespace, dot and
// non-Unicode /i case folding (not RE2's Unicode folding).
func TestOverflowJavaScriptRegexSemantics(t *testing.T) {
	for _, tc := range []struct {
		provider, text string
		want           bool
	}{
		{"cerebras", "400\u00a0status code\ufeff(no body)", true},
		{"openai", "exceeds maximum context length\u00a0(200000)", true},
		{"google", "input token count\rexceeds the maximum", false},
		{"google", "input token count\u2028exceeds the maximum", false},
		{"google", "input token count\u2029exceeds the maximum", false},
		{"anthropic", "prompt iſ too long", false},
		{"anthropic", "PROMPT IS TOO LONG", true},
	} {
		msg := &AssistantMessage{Provider: tc.provider, StopReason: StopError, ErrorMessage: &tc.text}
		if got := IsContextOverflow(msg, 0); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.text, got, tc.want)
		}
	}
}

// Reference: pi v1.5.0 (f9a26365), utils/overflow.ts `isContextOverflow`, with
// every expectation produced by running it. The corpus covers each provider
// error string the reference documents, the non-overflow messages that must win
// over them, case and Unicode lookalike variants of a few strings (the port
// emulates the reference's case-insensitive matching over ASCII letters), and
// the two silent-overflow signals: usage past the context window, and a length
// stop with no output filling it.
//
// Regenerate: node --experimental-strip-types testdata/overflow-classify/generate.mjs ../pi > testdata/overflow-classify/golden.json
func TestOverflowClassificationMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/overflow-classify/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Provider      string `json:"provider"`
		StopReason    string `json:"stopReason"`
		ErrorMessage  string `json:"errorMessage"`
		ContextWindow int64  `json:"contextWindow"`
		Want          bool   `json:"want"`
		Usage         struct {
			Input     int64 `json:"input"`
			Output    int64 `json:"output"`
			CacheRead int64 `json:"cacheRead"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 200 {
		t.Fatalf("fixture has %d cases, expected the full corpus", len(cases))
	}
	for _, testCase := range cases {
		stopReason := StopStop
		switch testCase.StopReason {
		case "error":
			stopReason = StopError
		case "length":
			stopReason = StopLength
		}
		message := &AssistantMessage{
			Provider: testCase.Provider, Model: "m", StopReason: stopReason,
			Usage: Usage{
				Input: testCase.Usage.Input, Output: testCase.Usage.Output, CacheRead: testCase.Usage.CacheRead,
			},
		}
		if testCase.ErrorMessage != "" {
			text := testCase.ErrorMessage
			message.ErrorMessage = &text
		}
		if got := IsContextOverflow(message, testCase.ContextWindow); got != testCase.Want {
			t.Errorf("%s stop=%s cw=%d %q = %v, want %v",
				testCase.Provider, testCase.StopReason, testCase.ContextWindow, testCase.ErrorMessage, got, testCase.Want)
		}
	}
}

// pi v1.4.0 utils/overflow.ts isContextOverflow, executed over provider error
// texts, near-misses (case folding, non-ASCII lookalikes, status mentions in
// the body) and the silent/length-stop usage cases. Regenerate:
// node ai/testdata/overflow/generate.mjs ./pi > ai/testdata/overflow/golden.json
func TestContextOverflowMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/overflow/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name         string  `json:"name"`
		Provider     string  `json:"provider"`
		StopReason   string  `json:"stopReason"`
		ErrorMessage *string `json:"errorMessage"`
		Usage        struct {
			Input     int64 `json:"input"`
			Output    int64 `json:"output"`
			CacheRead int64 `json:"cacheRead"`
		} `json:"usage"`
		ContextWindow *int64 `json:"contextWindow"`
		Expected      bool   `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty overflow corpus")
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			message := &AssistantMessage{
				Provider: test.Provider, StopReason: test.StopReason, ErrorMessage: test.ErrorMessage,
				Usage: Usage{Input: test.Usage.Input, Output: test.Usage.Output, CacheRead: test.Usage.CacheRead},
			}
			window := int64(0)
			if test.ContextWindow != nil {
				window = *test.ContextWindow
			}
			if got := IsContextOverflow(message, window); got != test.Expected {
				t.Errorf("IsContextOverflow(%q) = %v, want %v", deref(test.ErrorMessage), got, test.Expected)
			}
		})
	}
}
