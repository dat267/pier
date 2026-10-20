package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// error-body.ts extractBody uses JavaScript String.trim before truncating.
func TestGrammarVariantUsesJavaScriptTrim(t *testing.T) {
	tool := Tool{
		Name:       "grammar",
		Parameters: json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`),
		ConstrainedSampling: ConstrainedSamplingValue{
			Set: true, Config: &ConstrainedSamplingConfig{Type: "grammar", Variants: map[string]string{"openai_lark": "\uFEFF"}},
		},
	}
	if _, err := ResolveGrammarConstrainedSampling(tool, true); err == nil {
		t.Fatal("BOM-only grammar definition must be treated as empty")
	}
}

func TestNormalizeProviderErrorTrimsJavaScriptWhitespace(t *testing.T) {
	normalized := NormalizeProviderError(&ProviderError{
		Status:  429,
		Body:    "\uFEFFbody\uFEFF",
		Message: "request failed",
	})
	if normalized.Body != "body" {
		t.Fatalf("body = %q; want JavaScript-trimmed body", normalized.Body)
	}
}

// A canceled or expired caller is an abort, not a provider failure. Upstream
// throws Error("Request was aborted") when the signal is aborted, so a wrapped
// context error must not reach the message as Go's "context canceled" (D256).
func TestNormalizeProviderErrorReportsCanceledRequestsAsAborts(t *testing.T) {
	for _, err := range []error{
		context.Canceled,
		context.DeadlineExceeded,
		fmt.Errorf("stream failed: %w", context.Canceled),
		fmt.Errorf("stream failed: %w", context.DeadlineExceeded),
	} {
		if got := NormalizeProviderError(err).Message; got != RequestAbortedMessage {
			t.Fatalf("%v: message = %q, want %q", err, got, RequestAbortedMessage)
		}
	}
}
