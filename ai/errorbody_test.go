package ai

import (
	"encoding/json"
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
