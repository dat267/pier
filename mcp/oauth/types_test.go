package oauth

import "testing"

// types.ts parses expires_in with Number(), so numeric strings and booleans
// coerce instead of rejecting the response.
func TestParseOAuthTokensCoercesExpiresInLikeNumber(t *testing.T) {
	base := map[string]any{"access_token": "a", "token_type": "Bearer"}
	for _, test := range []struct {
		value any
		want  int64
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
			t.Fatalf("expires_in %#v -> %+v, %v; want %d", test.value, tokens, err, test.want)
		}
	}
	// absent() treats "" like undefined, so it means no expiry rather than 0.
	empty, err := ParseOAuthTokens(map[string]any{"access_token": "a", "token_type": "Bearer", "expires_in": ""})
	if err != nil || empty.ExpiresIn != nil {
		t.Fatalf("expires_in %q -> %+v, %v; want no expiry", "", empty, err)
	}
	for _, invalid := range []any{"soon", "Infinity", "NaN"} {
		response := map[string]any{"access_token": "a", "token_type": "Bearer", "expires_in": invalid}
		if _, err := ParseOAuthTokens(response); err == nil || err.Error() != "Invalid expires_in" {
			t.Fatalf("expires_in %#v err = %v; want Invalid expires_in", invalid, err)
		}
	}
}
