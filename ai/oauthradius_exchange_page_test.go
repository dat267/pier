package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Reference: pi v1.5.2 (b4afdddf) auth/oauth/radius.ts gives the callback
// server a complete hook that exchanges the code, so the browser sees
// "${providerName} sign-in failed." and the wait rejects when the exchange
// fails. Pier exchanged after the wait and had already answered the browser
// with the success page.
func TestRadiusBrowserLoginReportsAFailedExchangeOnTheBrowserPage(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/oauth":
			_, _ = writer.Write([]byte(`{"authorizationEndpoint":"http://` + request.Host + `/authorize"}`))
		case "/v1/oauth/token":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"invalid_grant","error_description":"code expired"}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gateway.Close()

	type loginResult struct {
		credential *OAuthCredential
		err        error
	}
	authorize := make(chan string, 1)
	done := make(chan loginResult, 1)
	go func() {
		credential, err := CreateRadiusOAuth(RadiusOAuthOptions{Name: "Radius", Gateway: gateway.URL}).Login(
			&AuthInteraction{
				Ctx:    context.Background(),
				Prompt: func(AuthPrompt) (string, error) { return RadiusLoginBrowser, nil },
				Notify: func(event AuthEvent) {
					if event.Type == AuthEventAuthURL {
						authorize <- event.URL
					}
				},
			})
		done <- loginResult{credential: credential, err: err}
	}()

	var authorizeURL string
	select {
	case authorizeURL = <-authorize:
	case <-time.After(5 * time.Second):
		t.Fatal("no authorize URL")
	}
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(RadiusRedirectURI + "?code=the-code&state=" + url.QueryEscape(parsed.Query().Get("state")))
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusBadGateway ||
		!strings.Contains(string(body), "Radius sign-in failed.") ||
		!strings.Contains(string(body), "code expired") {
		t.Fatalf("callback page status %d body %q", response.StatusCode, string(body))
	}

	select {
	case result := <-done:
		if result.err == nil || !strings.Contains(result.err.Error(), "Radius OAuth token request failed: invalid_grant: code expired") {
			t.Fatalf("login = %+v, %v", result.credential, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the login did not finish")
	}
}
