package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) auth/oauth/anthropic.ts. `anthropicOAuth.login`
// first asks for a login method ("Browser login (default)" / "Copy code login
// (headless)"), and the headless path authorizes against
// COPY_CODE_REDIRECT_URI ("https://platform.claude.com/oauth/code/callback") and
// exchanges the pasted `code#state` with that same redirect URI. The port went
// straight to the browser flow, so a headless login was impossible.
func TestAnthropicOAuthOffersTheCopyCodeLogin(t *testing.T) {
	var tokenRequests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		fields := map[string]any{}
		_ = json.Unmarshal(raw, &fields)
		tokenRequests = append(tokenRequests, fields)
		_, _ = writer.Write([]byte(`{"access_token":"a","refresh_token":"r","expires_in":3600}`))
	}))
	defer server.Close()

	previous := anthropicTokenURLValue
	anthropicTokenURLValue = server.URL
	defer func() { anthropicTokenURLValue = previous }()

	var notified []AuthEvent
	prompts := 0
	credential, err := AnthropicOAuth().Login(&AuthInteraction{
		Ctx: context.Background(),
		Prompt: func(prompt AuthPrompt) (string, error) {
			prompts++
			if prompt.Type == AuthPromptSelect {
				if prompt.Message != "Select Anthropic login method:" || len(prompt.SelectOptions) != 2 {
					t.Fatalf("select prompt = %+v", prompt)
				}
				return AnthropicCopyCodeLogin, nil
			}
			if prompt.Type != AuthPromptManualCode {
				t.Fatalf("prompt type = %q", prompt.Type)
			}
			// The headless path asks for the code Anthropic shows.
			if prompt.Placeholder != "code#state" {
				t.Fatalf("placeholder = %q, want code#state", prompt.Placeholder)
			}
			return "the-code#" + verifierFromAuthorizeURL(t, notified), nil
		},
		Notify: func(event AuthEvent) { notified = append(notified, event) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompts != 2 {
		t.Fatalf("prompts = %d, want the method select and the code prompt", prompts)
	}
	if credential.Access != "a" || credential.Refresh != "r" {
		t.Fatalf("credential = %+v", credential)
	}
	if len(notified) != 2 || notified[0].Type != AuthEventAuthURL {
		t.Fatalf("notified = %+v", notified)
	}
	authorize, err := url.Parse(notified[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorize.Query().Get("redirect_uri"); got != AnthropicCopyCodeRedirectURI {
		t.Fatalf("authorize redirect_uri = %q, want %q", got, AnthropicCopyCodeRedirectURI)
	}
	state := authorize.Query().Get("state")
	if state == "" {
		t.Fatal("authorize URL has no state")
	}
	if len(tokenRequests) != 1 {
		t.Fatalf("token requests = %d", len(tokenRequests))
	}
	exchange := tokenRequests[0]
	if exchange["code"] != "the-code" || exchange["state"] != state {
		t.Fatalf("exchange = %+v", exchange)
	}
	if exchange["redirect_uri"] != AnthropicCopyCodeRedirectURI {
		t.Fatalf("exchange redirect_uri = %v, want %s", exchange["redirect_uri"], AnthropicCopyCodeRedirectURI)
	}
}

func verifierFromAuthorizeURL(t *testing.T, notified []AuthEvent) string {
	t.Helper()
	if len(notified) == 0 || notified[0].Type != AuthEventAuthURL {
		t.Fatalf("no authorize URL: %+v", notified)
	}
	parsed, err := url.Parse(notified[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get("state")
}

// The browser method stays the default and reports the loopback redirect URI.
func TestAnthropicOAuthBrowserLoginIsTheDefault(t *testing.T) {
	var notified []AuthEvent
	_, err := AnthropicOAuth().Login(&AuthInteraction{
		Ctx: context.Background(),
		Prompt: func(prompt AuthPrompt) (string, error) {
			if prompt.Type == AuthPromptSelect {
				return AnthropicBrowserLogin, nil
			}
			return "", context.Canceled
		},
		Notify: func(event AuthEvent) { notified = append(notified, event) },
	})
	if err == nil {
		t.Fatal("a cancelled manual prompt must fail the login")
	}
	if len(notified) == 0 || notified[0].Type != AuthEventAuthURL {
		t.Fatalf("notified = %+v", notified)
	}
	parsed, parseErr := url.Parse(notified[0].URL)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if got := parsed.Query().Get("redirect_uri"); got != AnthropicRedirectURI && !strings.HasPrefix(got, "http://localhost:") {
		t.Fatalf("browser redirect_uri = %q", got)
	}
}

func TestAnthropicOAuthRejectsAnUnknownLoginMethod(t *testing.T) {
	_, err := AnthropicOAuth().Login(&AuthInteraction{
		Ctx:    context.Background(),
		Prompt: func(AuthPrompt) (string, error) { return "nope", nil },
	})
	if err == nil || err.Error() != "Unknown Anthropic login method: nope" {
		t.Fatalf("err = %v", err)
	}
}
