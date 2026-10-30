package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pi v1.4.0 auth/oauth/anthropic.ts computes expires with JavaScript number
// arithmetic, which keeps a very large negative lifetime in the past.
func TestAnthropicTokenExpiryKeepsLargeNegativeLifetimeInPast(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":-15000000000000000}`))
	}))
	defer server.Close()

	withTokenURL(t, server.URL, func() {
		before := time.Now().UnixMilli()
		credential, err := ExchangeAnthropicAuthorizationCode(context.Background(), "code", "state", "verifier", AnthropicRedirectURI)
		if err != nil {
			t.Fatal(err)
		}
		if credential.Expires >= before {
			t.Fatalf("credential expiry = %d, should remain past for large negative expires_in", credential.Expires)
		}
	})
}

func TestOAuthExpiryRemainsFutureAcrossTokenParsers(t *testing.T) {
	const largeLifetime = float64(15_000_000_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/oauth/token":
			_, _ = writer.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":15000000000000000}`))
		case "/copilot":
			_, _ = writer.Write([]byte(`{"token":"copilot","expires_at":15000000000000000}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	oldURLsBuilder := githubCopilotURLsBuilder
	githubCopilotURLsBuilder = func(string) GitHubCopilotURLs {
		return GitHubCopilotURLs{CopilotTokenURL: server.URL + "/copilot"}
	}
	t.Cleanup(func() { githubCopilotURLsBuilder = oldURLsBuilder })

	cases := []struct {
		name   string
		expiry func() (int64, error)
	}{
		{name: "xAI", expiry: func() (int64, error) {
			credential, err := xaiCredentialsFromTokenResponse(map[string]any{
				"access_token": "access", "refresh_token": "refresh", "expires_in": largeLifetime,
			}, "")
			if err != nil {
				return 0, err
			}
			return credential.Expires, nil
		}},
		{name: "Kimi", expiry: func() (int64, error) {
			token, err := parseKimiTokenResponse(map[string]any{
				"access_token": "access", "refresh_token": "refresh", "expires_in": largeLifetime,
			}, "refresh")
			if err != nil {
				return 0, err
			}
			return token.Expires, nil
		}},
		{name: "OpenAI Codex", expiry: func() (int64, error) {
			response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
				`{"access_token":"access","refresh_token":"refresh","expires_in":15000000000000000}`))}
			token, err := ReadOpenAICodexTokenResponse(response, "refresh")
			_ = response.Body.Close()
			if err != nil {
				return 0, err
			}
			return token.Expires, nil
		}},
		{name: "OpenAI ChatGPT", expiry: func() (int64, error) {
			credential, err := openAIChatGPTCredentialFromToken(&openAIChatGPTTokenResponse{
				AccessToken: "access", RefreshToken: "refresh", Scope: OpenAIChatGPTDirectTokenScope,
				ExpiresIn: largeLifetime, HasExpiresInFlag: true,
			}, "client")
			if err != nil {
				return 0, err
			}
			return credential.Expires, nil
		}},
		{name: "Radius", expiry: func() (int64, error) {
			credential, err := RequestRadiusOAuthToken(context.Background(), server.URL, nil)
			if err != nil {
				return 0, err
			}
			return credential.Expires, nil
		}},
		{name: "GitHub Copilot", expiry: func() (int64, error) {
			credential, err := RefreshGitHubCopilotAccessToken(context.Background(), "refresh", "")
			if err != nil {
				return 0, err
			}
			return credential.Expires, nil
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := time.Now().UnixMilli()
			expires, err := test.expiry()
			if err != nil {
				t.Fatal(err)
			}
			if expires <= before {
				t.Fatalf("expiry = %d, should be future for large positive JS timestamp", expires)
			}
		})
	}
}

// A very large positive lifetime remains future in upstream's JS arithmetic.
func TestAnthropicTokenExpiryDoesNotWrapForLargeLifetime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":15000000000000000}`))
	}))
	defer server.Close()

	withTokenURL(t, server.URL, func() {
		before := time.Now().UnixMilli()
		credential, err := ExchangeAnthropicAuthorizationCode(context.Background(), "code", "state", "verifier", AnthropicRedirectURI)
		if err != nil {
			t.Fatal(err)
		}
		if credential.Expires <= before {
			t.Fatalf("credential expiry = %d, should remain future for large expires_in", credential.Expires)
		}
		refreshed, err := RefreshAnthropicToken(context.Background(), "refresh")
		if err != nil {
			t.Fatal(err)
		}
		if refreshed.Expires <= before {
			t.Fatalf("refreshed credential expiry = %d, should remain future for large expires_in", refreshed.Expires)
		}
	})
}
