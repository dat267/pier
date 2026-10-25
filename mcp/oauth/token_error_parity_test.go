package oauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// pi v1.4.0 packages/mcp/src/oauth/flow.ts tokenRequest checks typeof
// value.error === "string", not truthiness, before checking the HTTP status.
func TestTokenRequestPreservesEmptyOAuthErrorCode(t *testing.T) {
	_, err := ExchangeAuthorizationCode(context.Background(), "https://auth.example", ExchangeOptions{
		TokenRequestOptions: TokenRequestOptions{
			ClientInformation: &OAuthClientInformationFull{OAuthClientInformation: OAuthClientInformation{ClientID: "fixture-client"}},
			Fetch: func(context.Context, *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":"","error_description":"fixture-description"}`))}, nil
			},
		},
		Code: "fixture-code", CodeVerifier: "fixture-verifier", RedirectURL: "http://127.0.0.1/callback",
	})
	var oauthError *OAuthError
	if !errors.As(err, &oauthError) || oauthError.Code != "" || oauthError.Msg != "fixture-description" {
		t.Fatalf("error = %#v, want empty OAuth error code and server description", err)
	}
}
