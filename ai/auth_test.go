package ai

import "testing"

// auth/context.ts defaultProviderAuthContext uses JavaScript String.trim.
func TestDefaultAuthContextTrimsJavaScriptWhitespace(t *testing.T) {
	t.Setenv("PIER_TEST_AUTH_BOM", "\uFEFF")
	if value, ok := (DefaultAuthContext{}).Env("PIER_TEST_AUTH_BOM"); ok || value != "" {
		t.Fatalf("Env() = %q, %v; want whitespace-only value unset", value, ok)
	}
}
