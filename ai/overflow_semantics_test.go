package ai

import "testing"

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
