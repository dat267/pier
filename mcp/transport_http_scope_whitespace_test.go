package mcp

import (
	"net/http"
	"testing"
)

// Reference: pi v1.4.0 (4a080252),
// packages/mcp/src/transports/streamable-http.ts needsAuthorization:
// /(?:^|[\s,])error="?insufficient_scope"?/i. JavaScript's \s includes the
// Unicode space separators, so a challenge separated by one is still a
// step-up authorization request.
func TestNeedsAuthorizationAcceptsJavaScriptWhitespace(t *testing.T) {
	for _, separator := range []struct {
		name  string
		value string
	}{
		{"ASCII space", " "},
		{"tab", "\t"},
		{"no-break space", "\u00a0"},
		{"ideographic space", "\u3000"},
		{"comma", ","},
	} {
		response := &http.Response{
			StatusCode: http.StatusForbidden,
			Header: http.Header{"Www-Authenticate": []string{
				`Bearer realm="mcp"` + separator.value + `error="insufficient_scope"`,
			}},
		}
		if !needsAuthorization(response) {
			t.Errorf("%s: needsAuthorization = false, want true", separator.name)
		}
	}
	// An unrelated 403 challenge is not a step-up request.
	other := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Www-Authenticate": []string{`Bearer realm="mcp"`}},
	}
	if needsAuthorization(other) {
		t.Error("needsAuthorization(unrelated 403) = true, want false")
	}
}
