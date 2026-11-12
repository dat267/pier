package oauth

import (
	"encoding/json"
	"testing"
)

// FuzzMetadataParsers feeds an authorization server's documents through the
// parsers: discovery metadata, a protected-resource document, token endpoint
// responses, client registration responses and a WWW-Authenticate challenge. A
// server is untrusted input, so a malformed document must be an error, never a
// panic.
func FuzzMetadataParsers(f *testing.F) {
	seeds := []string{
		`{"authorization_endpoint":"https://a/authorize","token_endpoint":"https://a/token"}`,
		`{"resource":"https://r","authorization_servers":["https://a"]}`,
		`{"access_token":"t","token_type":"Bearer","expires_in":3600,"refresh_token":"r","scope":"s"}`,
		`{"access_token":"t","expires_in":"3600"}`,
		`{"client_id":"c","client_secret":"s","redirect_uris":["https://cb"]}`,
		`{}`, `[]`, `null`, `{"expires_in":1e400}`, `{"authorization_servers":"x"}`,
		`Bearer realm="x", error="invalid_token", error_description="bad"`,
		`Basic`, ``, `realm`, `Bearer`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		_ = ParseWwwAuthenticate(data)
		var value any
		if err := json.Unmarshal([]byte(data), &value); err != nil {
			return
		}
		_, _ = ParseProtectedResourceMetadata(value)
		_, _ = ParseAuthorizationServerMetadata(value)
		_, _ = ParseOAuthTokens(value)
		_, _ = ParseClientInformation(value)
	})
}
