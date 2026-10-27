package oauth

import (
	"net/url"
	"testing"
)

// secureEndpoint must treat loopback hosts case-insensitively, like the WHATWG
// URL parser's lowercased hostname: HTTP is allowed on loopback only.
// packages/mcp/src/oauth/flow.ts selects an explicit supported hint before
// applyClientAuthentication reports a missing client secret.
func TestSelectClientAuthMethodHonorsBasicHintWithoutSecret(t *testing.T) {
	hint := ClientAuthSecretBasic
	information := &OAuthClientInformationFull{
		ClientID:                "client",
		TokenEndpointAuthMethod: &hint,
	}
	if got := selectClientAuthMethod(information, nil); got != ClientAuthSecretBasic {
		t.Fatalf("auth method = %q; want explicit basic hint", got)
	}
	err := applyClientAuthentication(selectClientAuthMethod(information, nil), &information.OAuthClientInformation, map[string]string{}, nil)
	if err == nil || err.Error() != "client_secret_basic requires a client secret" {
		t.Fatalf("applyClientAuthentication error = %v; want missing-secret error", err)
	}
}

// packages/mcp/src/oauth/flow.ts uses truthy checks for optional URL parameters.
func TestStartAuthorizationOmitsEmptyOptionalParameters(t *testing.T) {
	empty := ""
	draft, err := StartAuthorization("https://auth.example", StartAuthorizationOptions{
		ClientInformation: &OAuthClientInformation{ClientID: "client"},
		RedirectURL:       "http://127.0.0.1/callback",
		State:             &empty,
		Scope:             &empty,
		Resource:          &empty,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(draft.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"state", "scope", "resource"} {
		if parsed.Query().Has(key) {
			t.Errorf("query contains empty %q parameter", key)
		}
	}
}

// flow.ts splits scopes with JavaScript /\\s+/, which treats BOM as whitespace.
func TestStartAuthorizationRecognizesOfflineAccessAroundBOM(t *testing.T) {
	scope := "\uFEFFoffline_access"
	draft, err := StartAuthorization("https://auth.example", StartAuthorizationOptions{
		ClientInformation: &OAuthClientInformation{ClientID: "client"},
		RedirectURL:       "http://127.0.0.1/callback",
		Scope:             &scope,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(draft.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("prompt") != "consent" {
		t.Fatalf("prompt = %q; want consent", parsed.Query().Get("prompt"))
	}
}

func TestWithScopeOmitsEmptyScope(t *testing.T) {
	empty := ""
	tokens := withScope(&OAuthTokens{AccessToken: "access"}, &empty)
	if tokens.Scope != nil {
		t.Fatalf("scope = %q; want nil for an empty requested scope", *tokens.Scope)
	}
}

func TestSecureEndpointLoopbackIsCaseInsensitive(t *testing.T) {
	for _, value := range []string{"http://localhost:8080/x", "http://LOCALHOST/x", "http://127.0.0.1/x", "http://[::1]/x"} {
		if _, err := secureEndpoint(value); err != nil {
			t.Errorf("secureEndpoint(%q) = %v, want allowed", value, err)
		}
	}
	if _, err := secureEndpoint("http://example.com/x"); err == nil {
		t.Error("secureEndpoint(http://example.com) accepted a non-loopback HTTP URL")
	}
}
