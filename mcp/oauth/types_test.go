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

// The reference formats both issuers with JSON.stringify (errors.ts:20), so a
// quote or control character in a server-supplied `iss` cannot break up the
// message. Plain concatenation printed `expected "a"b"`.
func TestOAuthIssuerMismatchErrorQuotesLikeJSON(t *testing.T) {
	cases := []struct {
		name string
		err  *OAuthIssuerMismatchError
		want string
	}{
		{
			name: "quote in expected",
			err:  &OAuthIssuerMismatchError{Expected: "a\"b"},
			want: `OAuth issuer mismatch: expected "a\"b", received none`,
		},
		{
			name: "both sides, newline",
			err:  &OAuthIssuerMismatchError{Expected: "x\ny", Received: new("z\"w")},
			want: `OAuth issuer mismatch: expected "x\ny", received "z\"w"`,
		},
		{
			// JSON.stringify leaves U+2028 and "<" raw; encoding/json escaped them.
			name: "line separator and html",
			err:  &OAuthIssuerMismatchError{Expected: "a\u2028<b>"},
			want: "OAuth issuer mismatch: expected \"a\u2028<b>\", received none",
		},
		{
			name: "explicit message wins",
			err:  &OAuthIssuerMismatchError{Expected: "a", Msg: "custom"},
			want: "custom",
		},
	}
	for _, tc := range cases {
		if got := tc.err.Error(); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/oauth/types.ts compact keeps every member
// that is not undefined, so a `null` unknown member survives the round trip:
// `Object.entries(value).filter(([, item]) => item !== undefined)`. The port
// dropped `nil` too, losing a member the server sent and the reference preserved
// (the value is persisted in the stored credential's Extra).
func TestUnknownMembersPreserveNullLikeCompact(t *testing.T) {
	input := map[string]any{
		"resource":    "https://a.example/m",
		"extra_null":  nil,
		"extra_value": "v",
		"extra_false": false,
	}
	parsed, err := ParseProtectedResourceMetadata(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.Extra["extra_null"]; !ok {
		t.Fatal("a null unknown member was dropped; compact keeps it")
	}
	if string(parsed.Extra["extra_null"]) != "null" {
		t.Fatalf("extra_null = %q, want null", parsed.Extra["extra_null"])
	}
	if _, ok := parsed.Extra["extra_value"]; !ok {
		t.Fatal("a string unknown member was dropped")
	}
	if _, ok := parsed.Extra["resource"]; ok {
		t.Fatal("a known member leaked into Extra")
	}
}
