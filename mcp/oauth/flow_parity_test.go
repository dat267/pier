package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

type flowRequest struct {
	URL           string `json:"url"`
	Method        string `json:"method"`
	ContentType   string `json:"contentType"`
	Authorization string `json:"authorization"`
	Body          string `json:"body"`
}

type flowFixture struct {
	Registration map[string]struct {
		Input struct {
			ClientMetadata OAuthClientMetadata `json:"clientMetadata"`
			Scope          *string             `json:"scope"`
		} `json:"input"`
		Request flowRequest `json:"request"`
	} `json:"registration"`
	Authorization map[string]struct {
		Input struct {
			ClientInformation OAuthClientInformation       `json:"clientInformation"`
			RedirectURL       string                       `json:"redirectUrl"`
			Scope             *string                      `json:"scope"`
			State             *string                      `json:"state"`
			Resource          *string                      `json:"resource"`
			Metadata          *AuthorizationServerMetadata `json:"metadata"`
		} `json:"input"`
		AuthorizationURL string `json:"authorizationUrl"`
		VerifierLength   int    `json:"verifierLength"`
	} `json:"authorization"`
	Tokens map[string]struct {
		Input struct {
			Kind              string                       `json:"kind"`
			ClientInformation OAuthClientInformation       `json:"clientInformation"`
			Code              string                       `json:"code"`
			CodeVerifier      string                       `json:"codeVerifier"`
			RedirectURL       string                       `json:"redirectUrl"`
			RefreshToken      string                       `json:"refreshToken"`
			Metadata          *AuthorizationServerMetadata `json:"metadata"`
			Resource          *string                      `json:"resource"`
		} `json:"input"`
		Request flowRequest `json:"request"`
	} `json:"tokens"`
}

// loadFlowFixture reads the source-generated pinned request fixtures.
// Regenerate with
// node mcp/oauth/testdata/flow/generate.mjs ./pi > mcp/oauth/testdata/flow/golden.json
func loadFlowFixture(t *testing.T) flowFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/flow/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture flowFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Registration) == 0 || len(fixture.Authorization) == 0 || len(fixture.Tokens) == 0 {
		t.Fatal("fixture is empty")
	}
	return fixture
}

// capturingFetch records the request and answers with responseBody.
func capturingFetch(t *testing.T, requests *[]flowRequest, responseBody string) McpFetch {
	t.Helper()
	return func(_ context.Context, request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		*requests = append(*requests, flowRequest{
			URL:           request.URL.String(),
			Method:        request.Method,
			ContentType:   request.Header.Get("Content-Type"),
			Authorization: request.Header.Get("Authorization"),
			Body:          string(body),
		})
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Header:     make(http.Header),
		}, nil
	}
}

func compareFlowRequest(t *testing.T, expected flowRequest, got flowRequest) {
	t.Helper()
	if got != expected {
		t.Errorf("request = %+v, want %+v", got, expected)
	}
}

// TestOAuthRegistrationRequestMatchesUpstream pins the dynamic-registration
// body: reference member order, application_type in place and a truthy scope.
func TestOAuthRegistrationRequestMatchesUpstream(t *testing.T) {
	fixture := loadFlowFixture(t)
	for name, test := range fixture.Registration {
		t.Run(name, func(t *testing.T) {
			var requests []flowRequest
			fetch := capturingFetch(t, &requests, `{"client_id":"cid","redirect_uris":["http://127.0.0.1:33418/callback"]}`)
			_, err := RegisterClient(context.Background(), "https://auth.example.com", RegisterClientOptions{
				ClientMetadata: test.Input.ClientMetadata,
				Scope:          test.Input.Scope,
				Fetch:          fetch,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(requests))
			}
			compareFlowRequest(t, test.Request, requests[0])
		})
	}
}

var challengePattern = regexp.MustCompile(`code_challenge=[^&]+`)

// TestOAuthAuthorizationURLMatchesUpstream pins the authorization query's
// assignment order and form escaping (the random challenge is masked).
func TestOAuthAuthorizationURLMatchesUpstream(t *testing.T) {
	fixture := loadFlowFixture(t)
	for name, test := range fixture.Authorization {
		t.Run(name, func(t *testing.T) {
			draft, err := StartAuthorization("https://auth.example.com", StartAuthorizationOptions{
				ClientInformation: &test.Input.ClientInformation,
				RedirectURL:       test.Input.RedirectURL,
				Scope:             test.Input.Scope,
				State:             test.Input.State,
				Resource:          test.Input.Resource,
				Metadata:          test.Input.Metadata,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := challengePattern.ReplaceAllString(draft.AuthorizationURL, "code_challenge=<x>")
			if got != test.AuthorizationURL {
				t.Errorf("authorization URL = %q, want %q", got, test.AuthorizationURL)
			}
			if len(draft.CodeVerifier) != test.VerifierLength {
				t.Errorf("verifier length = %d, want %d", len(draft.CodeVerifier), test.VerifierLength)
			}
		})
	}
}

// TestOAuthTokenRequestMatchesUpstream pins the token form body order and
// client authentication.
func TestOAuthTokenRequestMatchesUpstream(t *testing.T) {
	fixture := loadFlowFixture(t)
	for name, test := range fixture.Tokens {
		t.Run(name, func(t *testing.T) {
			var requests []flowRequest
			fetch := capturingFetch(t, &requests, `{"access_token":"at","token_type":"Bearer","expires_in":3600}`)
			options := TokenRequestOptions{
				Metadata:          test.Input.Metadata,
				ClientInformation: &OAuthClientInformationFull{OAuthClientInformation: test.Input.ClientInformation},
				Resource:          test.Input.Resource,
				Fetch:             fetch,
			}
			var err error
			if test.Input.Kind == "exchange" {
				_, err = ExchangeAuthorizationCode(context.Background(), "https://auth.example.com", ExchangeOptions{
					TokenRequestOptions: options,
					Code:                test.Input.Code,
					CodeVerifier:        test.Input.CodeVerifier,
					RedirectURL:         test.Input.RedirectURL,
				})
			} else {
				_, err = RefreshAuthorization(context.Background(), "https://auth.example.com", RefreshOptions{
					TokenRequestOptions: options,
					RefreshToken:        test.Input.RefreshToken,
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(requests))
			}
			compareFlowRequest(t, test.Request, requests[0])
		})
	}
}

var _ = fmt.Sprintf
