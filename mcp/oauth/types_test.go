package oauth

import "testing"

// types.ts parses expires_in with Number(), so numeric strings and booleans
// coerce instead of rejecting the response.
func TestParseOAuthTokensCoercesExpiresInLikeNumber(t *testing.T) {
	base := map[string]any{"access_token": "a", "token_type": "Bearer"}
	for _, test := range []struct {
		value any
		want  float64
	}{
		{"3600", 3600},
		{" 3600 ", 3600},
		{"0x10", 16},
		{true, 1},
		{false, 0},
	} {
		response := map[string]any{"access_token": base["access_token"], "token_type": base["token_type"], "expires_in": test.value}
		tokens, err := ParseOAuthTokens(response)
		if err != nil || tokens.ExpiresIn == nil || *tokens.ExpiresIn != test.want {
			t.Fatalf("expires_in %#v -> %+v, %v; want %v", test.value, tokens, err, test.want)
		}
	}
	// absent() treats "" like undefined, so it means no expiry rather than 0.
	empty, err := ParseOAuthTokens(map[string]any{"access_token": "a", "token_type": "Bearer", "expires_in": ""})
	if err != nil || empty.ExpiresIn != nil {
		t.Fatalf("expires_in %q -> %+v, %v; want no expiry", "", empty, err)
	}
	// Number() has no numeric separators: "1_0" is NaN there, while Go's
	// ParseFloat reads it as 10.
	for _, invalid := range []any{"soon", "Infinity", "NaN", "1_0", "+1_0", "0_1", "1_0.5", "1_0e1"} {
		response := map[string]any{"access_token": "a", "token_type": "Bearer", "expires_in": invalid}
		if _, err := ParseOAuthTokens(response); err == nil || err.Error() != "Invalid expires_in" {
			t.Fatalf("expires_in %#v err = %v; want Invalid expires_in", invalid, err)
		}
	}
}

// pi v1.5.0 types.ts safeUrl accepts only http(s): every metadata URL is a
// candidate for the platform URL handler, which would also launch file: and
// custom protocols.
func TestParseAuthorizationServerMetadataTrustsOnlyHTTPURLs(t *testing.T) {
	metadata := func(endpoint string) map[string]any {
		return map[string]any{
			"response_types_supported": []any{"code"},
			"issuer":                   "https://auth.example",
			"authorization_endpoint":   endpoint,
			"token_endpoint":           "https://auth.example/token",
		}
	}
	for _, endpoint := range []string{"file:///etc/passwd", "ftp://auth.example/authorize", "urn:example:authorize"} {
		if _, err := ParseAuthorizationServerMetadata(metadata(endpoint)); err == nil ||
			err.Error() != "Invalid authorization endpoint" {
			t.Fatalf("%q err = %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"https://auth.example/authorize", "http://localhost:8080/authorize"} {
		if _, err := ParseAuthorizationServerMetadata(metadata(endpoint)); err != nil {
			t.Fatalf("%q err = %v", endpoint, err)
		}
	}
}
