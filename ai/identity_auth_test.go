package ai

import "testing"

// Reference: pi v1.4.0 models.ts applyAuth (04a93bad8). Auth may change
// the target host; session defaults precede auth and request headers, and
// TransformHeaders sees the result exactly once.
func TestResolvedAuthOpenCodeSessionHeaders(t *testing.T) {
	model := &Model{Provider: "custom", BaseURL: "https://example.com"}
	auth := ModelAuth{BaseURL: "https://opencode.ai/zen/v1"}
	for _, tc := range []struct {
		name           string
		authHeaders    ProviderHeaders
		requestHeaders ProviderHeaders
		want           *string
	}{
		{"session default", nil, nil, new("session-1")},
		{"auth override", ProviderHeaders{"X-OpenCode-Session": new("auth")}, nil, new("auth")},
		{"caller override", ProviderHeaders{"X-OpenCode-Session": new("auth")}, ProviderHeaders{"x-opencode-session": new("caller")}, new("caller")},
		{"caller removal", nil, ProviderHeaders{"X-OpenCode-Session": nil}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth.Headers = tc.authHeaders
			calls := 0
			requestModel, options := ApplyResolvedAuth(model, auth, nil, StreamOptions{
				SessionID: "session-1", Headers: tc.requestHeaders,
				TransformHeaders: func(headers ProviderHeaders) ProviderHeaders {
					calls++
					found := false
					for name, value := range headers {
						if lowerName(name) != "x-opencode-session" {
							continue
						}
						if found {
							t.Fatal("duplicate session header")
						}
						found = true
						if (value == nil) != (tc.want == nil) || value != nil && *value != *tc.want {
							t.Fatalf("session header = %v; want %v", value, tc.want)
						}
					}
					if !found {
						t.Fatal("session header missing")
					}
					return headers
				},
			})
			if requestModel.BaseURL != auth.BaseURL || model.BaseURL != "https://example.com" || calls != 1 || options.TransformHeaders != nil {
				t.Fatalf("model = %+v, transform calls = %d", requestModel, calls)
			}
		})
	}
}
