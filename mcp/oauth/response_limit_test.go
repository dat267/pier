package oauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// M35: successful OAuth documents come from a configured authorization server,
// which is untrusted network input. Oversized bodies must fail as protocol
// errors instead of being buffered whole.
const oversizedOAuthBody = 2 << 20

func TestDiscoverProtectedResourceMetadataRejectsOversizedBody(t *testing.T) {
	fetch := func(_ context.Context, _ *http.Request) (*http.Response, error) {
		body := `{"resource":"https://example.com/mcp","authorization_servers":[]}` + strings.Repeat(" ", oversizedOAuthBody)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	if metadata, err := DiscoverProtectedResourceMetadata(context.Background(), "https://example.com/mcp", DiscoveryOptions{Fetch: fetch}); err == nil || metadata != nil {
		t.Fatalf("oversized metadata accepted: %+v err=%v", metadata, err)
	}
}

func TestExchangeAuthorizationCodeRejectsOversizedBody(t *testing.T) {
	fetch := func(_ context.Context, _ *http.Request) (*http.Response, error) {
		body := `{"access_token":"` + strings.Repeat("x", oversizedOAuthBody) + `","token_type":"Bearer"}`
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	server := &AuthorizationServerMetadata{Issuer: "https://example.com", TokenEndpoint: "https://example.com/token"}
	metadata := server
	tokens, err := ExchangeAuthorizationCode(context.Background(), "https://example.com", ExchangeOptions{
		TokenRequestOptions: TokenRequestOptions{Metadata: metadata, Fetch: fetch, ClientInformation: &OAuthClientInformationFull{ClientID: "client"}},
		Code:                "code",
		CodeVerifier:        "verifier",
		RedirectURL:         "http://127.0.0.1/callback",
	})
	if err == nil || tokens != nil {
		t.Fatalf("oversized token response accepted")
	}
}

func TestRegisterClientRejectsOversizedBody(t *testing.T) {
	fetch := func(_ context.Context, _ *http.Request) (*http.Response, error) {
		body := `{"client_id":"client","padding":"` + strings.Repeat("x", oversizedOAuthBody) + `"}`
		return &http.Response{StatusCode: http.StatusCreated, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	registrationEndpoint := "https://example.com/register"
	metadata := &AuthorizationServerMetadata{Issuer: "https://example.com", RegistrationEndpoint: &registrationEndpoint}
	if client, err := RegisterClient(context.Background(), "https://example.com", RegisterClientOptions{
		Metadata:       metadata,
		ClientMetadata: OAuthClientMetadata{RedirectURIs: []string{"https://example.com/cb"}},
		Fetch:          fetch,
	}); err == nil || client != nil {
		t.Fatalf("oversized registration response accepted")
	}
}
