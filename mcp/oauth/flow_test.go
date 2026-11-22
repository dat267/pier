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

// pi v1.5.2 (b4afdddf) packages/mcp/src/oauth/flow.ts selectClientAuthMethod, each
// branch: a supported hint wins when it is one of the three known methods and the
// server lists it (or lists nothing); otherwise a secret prefers basic, then post,
// then none, and a secretless client is none unless the server only offers post.
// The fixture set in testdata/token-auth does not cross these, so the branches are
// pinned here.
func TestSelectClientAuthMethodCrossesEveryBranch(t *testing.T) {
	secret := func(value string) *string { return &value }
	hint := func(value string) *string { return &value }
	for _, tc := range []struct {
		name      string
		secret    *string
		hint      *string
		supported []string
		want      string
	}{
		{"no secret, no hint, no support list", nil, nil, nil, ClientAuthNone},
		{"secret, no hint, no support list", secret("s"), nil, nil, ClientAuthSecretBasic},
		{"no secret, basic hint, no support list", nil, hint(ClientAuthSecretBasic), nil, ClientAuthSecretBasic},
		{"a hint the server does not list falls through", nil, hint(ClientAuthSecretBasic), []string{ClientAuthSecretPost}, ClientAuthNone},
		{"an unknown hint falls through", secret("s"), hint("bogus"), nil, ClientAuthSecretBasic},
		{"secret prefers basic when both are offered", secret("s"), nil, []string{ClientAuthSecretPost, ClientAuthSecretBasic}, ClientAuthSecretBasic},
		{"secret takes post when basic is absent", secret("s"), nil, []string{ClientAuthSecretPost}, ClientAuthSecretPost},
		{"secret takes none when only none is offered", secret("s"), nil, []string{ClientAuthNone}, ClientAuthNone},
		{"no secret takes none when the server offers it", nil, nil, []string{ClientAuthSecretBasic, ClientAuthNone}, ClientAuthNone},
		{"no secret with only post offered is none", nil, nil, []string{ClientAuthSecretPost}, ClientAuthNone},
		{"listed hint wins over the secret preference", secret("s"), hint(ClientAuthNone), []string{ClientAuthNone}, ClientAuthNone},
	} {
		information := &OAuthClientInformationFull{ClientID: "id", ClientSecret: tc.secret, TokenEndpointAuthMethod: tc.hint}
		if got := selectClientAuthMethod(information, tc.supported); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// parseHTTPURL is pier-only: the reference relies on the WHATWG URL parser
// (packages/mcp/src/oauth/flow.ts). Node's URL accepts port 65535 and rejects
// 65536, keeps an explicit 0, and repairs a missing or backslashed authority; the
// port must agree on each, because the value decides whether credentials are sent.
func TestParseHTTPURLMatchesWHATWGDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		in      string
		host    string
		port    string
		wantErr bool
	}{
		{in: "https://a.example:65535/", host: "a.example", port: "65535"},
		{in: "https://a.example:0/", host: "a.example", port: "0"},
		{in: "https://a.example:65536/", wantErr: true},
		{in: "https://a.example:99999/", wantErr: true},
		{in: "http:a.example/x", host: "a.example"},
		{"https:\\\\a.example\\x", "a.example", "", false},
		{in: "https://", wantErr: true},
		{in: "https:/", wantErr: true},
		{in: "https://:443/x", wantErr: true},
		{in: "ftp://a.example/", wantErr: true},
	} {
		parsed, err := parseHTTPURL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: got %v, want an error", tc.in, parsed)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if parsed.Hostname() != tc.host || parsed.Port() != tc.port {
			t.Errorf("%q: host %q port %q, want %q %q", tc.in, parsed.Hostname(), parsed.Port(), tc.host, tc.port)
		}
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/oauth/flow.ts deriveApplicationType applies
// MCP SEP-837: a custom scheme or a loopback http(s) URL identifies a native
// client, anything else is web. The repaired-authority and IPv6 forms go through
// parseHTTPURL, which is where they can differ from the reference's WHATWG parser.
func TestDeriveApplicationTypeFollowsSEP837(t *testing.T) {
	for _, tc := range []struct {
		redirectURIs []string
		want         string
	}{
		{[]string{"http://127.0.0.1/cb"}, "native"},
		{[]string{"http://localhost:1234/cb"}, "native"},
		{[]string{"http://[::1]/cb"}, "native"},
		{[]string{"http:\\\\localhost/cb"}, "native"},
		{[]string{"com.example.app:/cb"}, "native"},
		{[]string{"https://a.example/cb"}, "web"},
		{[]string{"https://a.example/cb", "http://127.0.0.1/cb"}, "native"},
		{nil, "web"},
		{[]string{""}, "web"},
	} {
		if got := deriveApplicationType(tc.redirectURIs); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.redirectURIs, got, tc.want)
		}
	}
}
