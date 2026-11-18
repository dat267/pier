package oauth

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), packages/mcp/src/oauth/flow.ts
// startAuthorization assigns into the endpoint's `URL.searchParams`, so its
// application/x-www-form-urlencoded parser percent-decodes each component and
// then UTF-8-decodes the bytes, replacing malformed sequences. Expectations
// read off Node's URLSearchParams.
func TestOrderedFormDecodesComponentsAsUTF8(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"x=%FF", "x=%EF%BF%BD"},
		{"x=%C3", "x=%EF%BF%BD"},
		{"x=%E2%82", "x=%EF%BF%BD"},
		{"x=%ED%A0%80", "x=%EF%BF%BD%EF%BF%BD%EF%BF%BD"},
		{"x=%C0%AF", "x=%EF%BF%BD%EF%BF%BD"},
		{"x=%41%FF%42", "x=A%EF%BF%BDB"},
		{"x=%E2%82ac", "x=%EF%BF%BDac"},
		{"x=%F0%9F%92%A9", "x=%F0%9F%92%A9"},
		{"x=%EF%BB%BF", "x=%EF%BB%BF"},
		{"x=%00", "x=%00"},
		{"x=%25", "x=%25"},
		{"x=%zz", "x=%25zz"},
		{"x=a+b", "x=a+b"},
	}
	for _, testCase := range cases {
		if got := NewOrderedForm(testCase.raw).Encode(); got != testCase.want {
			t.Errorf("NewOrderedForm(%q).Encode() = %q, want %q", testCase.raw, got, testCase.want)
		}
	}
}

// The same decoding reaches the authorization URL when the advertised endpoint
// carries such a query.
func TestStartAuthorizationDecodesEndpointQueryAsUTF8(t *testing.T) {
	draft, err := StartAuthorization("https://as.example", StartAuthorizationOptions{
		Metadata: &AuthorizationServerMetadata{
			AuthorizationEndpoint:  "https://as.example/authorize?x=%FF",
			ResponseTypesSupported: []string{"code"},
		},
		ClientInformation: &OAuthClientInformation{ClientID: "client"},
		RedirectURL:       "http://127.0.0.1:1234/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(draft.AuthorizationURL, "x=%EF%BF%BD") {
		t.Fatalf("authorization URL = %q, want the decoded query value", draft.AuthorizationURL)
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/oauth/discovery.ts builds its
// well-known URLs from WHATWG `URL.pathname`, which drops dot segments while
// parsing, including the `%2e` spellings. The corpus is Node's own output for a
// path matrix; generate.mjs regenerates it.
func TestNormalizeDotSegmentsMatchesWHATWG(t *testing.T) {
	raw, err := os.ReadFile("testdata/dot-segments/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Path     string `json:"path"`
			Pathname string `json:"pathname"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) == 0 {
		t.Fatal("empty dot-segment fixture")
	}
	for _, row := range fixture.Rows {
		if got := normalizeDotSegments(row.Path); got != row.Pathname {
			t.Errorf("normalizeDotSegments(%q) = %q, want %q", row.Path, got, row.Pathname)
		}
	}
}

// D276: pi v1.5.0 (f9a26365) packages/mcp/src/oauth/discovery.ts
// resourceUrlFromServerUrl returns a WHATWG URL, which drops the default port
// and lowercases the host (`https://example.com:443/mcp` serializes as
// `https://example.com/mcp`). The Go helper keeps both, so the parse golden
// excludes these outputs. What upstream decides from them, SelectResource's
// origin and path match, must still agree.
func TestResourceURLKeepsDefaultPortAndHostCase(t *testing.T) {
	for _, test := range []struct{ server, want, upstream string }{
		{"https://example.com:443/mcp", "https://example.com:443/mcp", "https://example.com/mcp"},
		{"https://EXAMPLE.COM/MCP#frag", "https://EXAMPLE.COM/MCP", "https://example.com/MCP"},
	} {
		got := ResourceURLFromServerURL(test.server)
		if got != test.want {
			t.Errorf("ResourceURLFromServerURL(%q) = %q, want %q (upstream %q)", test.server, got, test.want, test.upstream)
		}
		// Upstream's selectResource matches the normalized form against the
		// metadata; the port's spelling and the host case must not change that.
		resource := "https://example.com/"
		selected, err := SelectResource(test.server, &OAuthProtectedResourceMetadata{Resource: resource})
		if err != nil || selected == nil || *selected != resource {
			t.Errorf("SelectResource(%q) = %v, %v; want %q as upstream", test.server, selected, err, resource)
		}
	}
}
