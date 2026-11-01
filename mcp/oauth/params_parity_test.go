package oauth

import (
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
