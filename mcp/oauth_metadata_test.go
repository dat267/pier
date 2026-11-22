package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dat267/pier/mcp/oauth"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Port of packages/mcp/test/oauth.test.ts: the full PKCE flow over the
// streamable-HTTP transport, refresh sharing, step-up, store binding,
// issuer validation, and the callback-server pages.

// testProvider is the Go port of TestOAuthProvider.
type testProvider struct {
	redirectURL      string
	clientMetadata   oauth.OAuthClientMetadata
	client           *oauth.OAuthClientInformationFull
	clientDocument   *oauth.OAuthClientMetadataDocument
	tokenSet         *oauth.OAuthTokens
	verifier         string
	discovery        *oauth.OAuthDiscoveryState
	authorizationURL string
}

func newTestProvider(redirectURL string) *testProvider {
	return &testProvider{
		redirectURL: redirectURL,
		clientMetadata: oauth.OAuthClientMetadata{
			RedirectURIs:            []string{redirectURL},
			ClientName:              new("pi-mcp-test"),
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: new("none"),
		},
	}
}

func (p *testProvider) RedirectURL() string { return p.redirectURL }

func (p *testProvider) ClientMetadataDocument(*oauth.AuthorizationServerMetadata) *oauth.OAuthClientMetadataDocument {
	return p.clientDocument
}

func (p *testProvider) State(context.Context) (string, error) { return "expected-state", nil }

func (p *testProvider) ClientInformation(context.Context) (*oauth.OAuthClientInformationFull, error) {
	if p.client == nil {
		return nil, nil
	}
	return p.client, nil
}

func (p *testProvider) SaveClientInformation(_ context.Context, information *oauth.OAuthClientInformationFull) error {
	p.client = information
	return nil
}

func (p *testProvider) RedirectToAuthorization(_ context.Context, rawURL string) error {
	p.authorizationURL = rawURL
	return nil
}

func (p *testProvider) SaveCodeVerifier(_ context.Context, verifier string) error {
	p.verifier = verifier
	return nil
}

func (p *testProvider) CodeVerifier(context.Context) (string, error) {
	if p.verifier == "" {
		return "", fmt.Errorf("Missing code verifier")
	}
	return p.verifier, nil
}

func (p *testProvider) AddClientAuthentication() oauth.AddClientAuthentication { return nil }

func (p *testProvider) InvalidateCredentials(_ context.Context, kind string) error {
	if kind == "all" || kind == "client" {
		p.client = nil
	}
	if kind == "all" || kind == "tokens" {
		p.tokenSet = nil
	}
	if kind == "all" || kind == "verifier" {
		p.verifier = ""
	}
	if kind == "all" || kind == "discovery" {
		p.discovery = nil
	}
	return nil
}

func (p *testProvider) SaveDiscoveryState(_ context.Context, state *oauth.OAuthDiscoveryState) error {
	p.discovery = state
	return nil
}

func (p *testProvider) DiscoveryState(context.Context) (*oauth.OAuthDiscoveryState, error) {
	return p.discovery, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	if status != 0 {
		w.WriteHeader(status)
	}
	w.Header().Set("content-type", "application/json")
	enc, _ := json.Marshal(payload)
	_, _ = w.Write(enc)
}

// oauthFixtureServer is the /mcp endpoint that demands a bearer token.
// awaitCallbackWaiter waits until WaitForCallback registered the state. The
// callback server drops a callback that arrives with no waiter (like upstream),
// so the redirect must be driven only after the waiter exists; otherwise a fast
// loopback request races the registration.
func awaitCallbackWaiter(t *testing.T, callback *oauth.OAuthCallbackServer, state string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !callback.Pending(state) {
		if time.Now().After(deadline) {
			t.Fatalf("callback waiter for %q never registered", state)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitCallbackAfterRequest registers the waiter, drives request, and returns the
// callback with the response.
func waitCallbackAfterRequest(t *testing.T, callback *oauth.OAuthCallbackServer, state string, request func() (*http.Response, error)) (oauth.OAuthCallback, *http.Response) {
	t.Helper()
	waiter := make(chan oauth.OAuthCallback, 1)
	waiterErr := make(chan error, 1)
	go func() {
		result, err := callback.WaitForCallback(state)
		if err != nil {
			waiterErr <- err
			return
		}
		waiter <- result
	}()
	awaitCallbackWaiter(t, callback, state)
	response, err := request()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-waiter:
		return result, response
	case err := <-waiterErr:
		t.Fatalf("callback: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("callback never resolved")
	}
	return oauth.OAuthCallback{}, nil
}

// waitCallbackErrorAfterRequest is the failure-path variant: the callback itself
// carries the error (a denied authorization).
func waitCallbackErrorAfterRequest(t *testing.T, callback *oauth.OAuthCallbackServer, state string, request func() (*http.Response, error)) (*http.Response, error) {
	t.Helper()
	waiterErr := make(chan error, 1)
	go func() { _, err := callback.WaitForCallback(state); waiterErr <- err }()
	awaitCallbackWaiter(t, callback, state)
	response, err := request()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waiterErr:
		return response, err
	case <-time.After(5 * time.Second):
		t.Fatal("callback never resolved")
	}
	return nil, nil
}

func errorsAs(err error, target any) bool {
	return errors.As(err, target)
}

// TestOAuthConfiguredMetadataDocument ports "uses a configured authorization
// server metadata document as is" (#10172).
func TestOAuthConfiguredMetadataDocument(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	origin := server.URL
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		// Names the MCP server itself, which serves no authorization server
		// metadata.
		writeJSON(w, 0, map[string]any{"resource": origin + "/mcp", "authorization_servers": []string{origin}})
	})
	mux.HandleFunc("/idp/metadata.json", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 0, map[string]any{
			// Not derivable from the document URL; a configured document is
			// not checked.
			"issuer": "https://idp.example", "authorization_endpoint": origin + "/idp/authorize",
			"token_endpoint": origin + "/idp/token", "response_types_supported": []string{"code"},
		})
	})
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.client = &oauth.OAuthClientInformationFull{ClientID: "client"}
	metadataURL := origin + "/idp/metadata.json"
	result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
		ServerURL: origin + "/mcp", AuthorizationServerMetadataURL: &metadataURL,
	})
	if err != nil || result != oauth.ResultRedirect {
		t.Fatalf("authorize = %s, %v", result, err)
	}
	authorizationURL, err := url.Parse(provider.authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if authorizationURL.Scheme+"://"+authorizationURL.Host+authorizationURL.Path != origin+"/idp/authorize" {
		t.Fatalf("authorization URL = %s", provider.authorizationURL)
	}
	if got := authorizationURL.Query().Get("resource"); got != origin+"/mcp" {
		t.Fatalf("resource = %q", got)
	}

	insecure := "http://idp.example/metadata.json"
	if _, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
		ServerURL: origin + "/mcp", AuthorizationServerMetadataURL: &insecure,
	}); err == nil {
		t.Fatal("insecure endpoint accepted")
	} else {
		var insecureErr *oauth.OAuthInsecureEndpointError
		if !errorsAs(err, &insecureErr) {
			t.Fatalf("err = %v (%T)", err, err)
		}
	}
}

// TestOAuthIssParameter ports "exchanges a code only when its iss parameter
// names the authorization server".
func TestOAuthIssParameter(t *testing.T) {
	var codesMu sync.Mutex
	var codes []string
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	origin := server.URL
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Discovery probes are GETs; without a metadata document they 404, as
		// upstream's test server does, so the flow runs with no metadata.
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		params, _ := url.ParseQuery(string(body))
		codesMu.Lock()
		codes = append(codes, params.Get("code"))
		codesMu.Unlock()
		writeJSON(w, 0, map[string]any{"access_token": "token", "token_type": "Bearer"})
	})
	exchange := func(code string, iss *string, issParameterSupported bool, metadata bool) (string, error) {
		provider := newTestProvider("http://127.0.0.1/callback")
		provider.client = &oauth.OAuthClientInformationFull{ClientID: "client"}
		provider.verifier = "verifier"
		provider.discovery = &oauth.OAuthDiscoveryState{AuthorizationServerURL: origin}
		if metadata {
			provider.discovery.AuthorizationServerMetadata = &oauth.AuthorizationServerMetadata{
				Issuer: origin, AuthorizationEndpoint: origin + "/authorize", TokenEndpoint: origin + "/token",
				ResponseTypesSupported:                     []string{"code"},
				AuthorizationResponseIssParameterSupported: new(issParameterSupported),
			}
		}
		return oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
			ServerURL: origin + "/mcp", AuthorizationCode: code, Iss: iss,
		})
	}
	attacker := "https://attacker.example"
	if _, err := exchange("other", &attacker, false, true); err == nil {
		t.Fatal("foreign iss accepted")
	}
	if _, err := exchange("missing", nil, true, true); err == nil {
		t.Fatal("missing iss accepted when the server promises the parameter")
	}
	if result, err := exchange("matching", &origin, true, true); err != nil || result != oauth.ResultAuthorized {
		t.Fatalf("matching = %s, %v", result, err)
	}
	// Servers that do not promise the parameter may omit it.
	if result, err := exchange("omitted", nil, false, true); err != nil || result != oauth.ResultAuthorized {
		t.Fatalf("omitted = %s, %v", result, err)
	}
	// Without metadata there is no issuer to compare with, so a server cannot
	// skip the check by hiding it.
	if _, err := exchange("unverifiable", &attacker, false, false); err == nil {
		t.Fatal("iss accepted without metadata")
	} else {
		var mismatchErr *oauth.OAuthIssuerMismatchError
		if !errorsAs(err, &mismatchErr) {
			t.Fatalf("err = %v (%T), want an issuer mismatch", err, err)
		}
	}
	if result, err := exchange("no-metadata", nil, false, false); err != nil || result != oauth.ResultAuthorized {
		t.Fatalf("no-metadata = %s, %v", result, err)
	}
	codesMu.Lock()
	defer codesMu.Unlock()
	if len(codes) != 3 || codes[0] != "matching" || codes[1] != "omitted" || codes[2] != "no-metadata" {
		t.Fatalf("codes = %v", codes)
	}
}

// TestOAuthCallbackPages ports the OAuthCallbackServer page tests.
func TestOAuthCallbackPages(t *testing.T) {
	t.Run("plain text by default", func(t *testing.T) {
		callback, err := oauth.ListenCallbackServer(context.Background(), oauth.OAuthCallbackServerOptions{TimeoutMs: 5000})
		if err != nil {
			t.Fatal(err)
		}
		defer callback.Close(context.Background())
		result, response := waitCallbackAfterRequest(t, callback, "s1", func() (*http.Response, error) {
			return http.Get(callback.RedirectURL + "?code=abc&state=s1")
		})
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if got := response.Header.Get("content-type"); got != "text/plain; charset=utf-8" {
			t.Fatalf("content-type = %q", got)
		}
		if string(body) != "Authorization complete. You may close this window." {
			t.Fatalf("page = %q", string(body))
		}
		if result.Code != "abc" {
			t.Fatalf("callback = %+v", result)
		}
	})
	t.Run("renders pages through renderPage", func(t *testing.T) {
		var pagesMu sync.Mutex
		var pages []oauth.OAuthCallbackPage
		callback, err := oauth.ListenCallbackServer(context.Background(), oauth.OAuthCallbackServerOptions{
			TimeoutMs: 5000,
			RenderPage: func(page oauth.OAuthCallbackPage) string {
				pagesMu.Lock()
				pages = append(pages, page)
				pagesMu.Unlock()
				if page.OK {
					return "<p>ok</p>"
				}
				return "<p>" + page.Message + "</p>"
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer callback.Close(context.Background())
		failure, deniedErr := waitCallbackErrorAfterRequest(t, callback, "s1", func() (*http.Response, error) {
			return http.Get(callback.RedirectURL + "?error=access_denied&error_description=Denied&state=s1")
		})
		if got := failure.Header.Get("content-type"); got != "text/html; charset=utf-8" {
			t.Fatalf("content-type = %q", got)
		}
		if deniedErr == nil || deniedErr.Error() != "Denied" {
			t.Fatalf("denied err = %v", deniedErr)
		}
		pagesMu.Lock()
		last := pages[len(pages)-1]
		pages = nil
		pagesMu.Unlock()
		if last.OK || last.Message != "Authorization failed. You may close this window." || last.Details == nil || *last.Details != "Denied" {
			t.Fatalf("page = %+v", last)
		}

		_, emptyDescriptionErr := waitCallbackErrorAfterRequest(t, callback, "s3", func() (*http.Response, error) {
			return http.Get(callback.RedirectURL + "?error=access_denied&error_description=&state=s3")
		})
		if emptyDescriptionErr == nil || emptyDescriptionErr.Error() != "" {
			t.Fatalf("empty error description = %v; want empty error text", emptyDescriptionErr)
		}

		result, success := waitCallbackAfterRequest(t, callback, "s2", func() (*http.Response, error) {
			return http.Get(callback.RedirectURL + "?code=abc&state=s2")
		})
		body, _ := io.ReadAll(success.Body)
		_ = success.Body.Close()
		if string(body) != "<p>ok</p>" {
			t.Fatalf("page = %q", string(body))
		}
		if result.Code != "abc" {
			t.Fatalf("callback = %+v", result)
		}
	})
}

// TestOAuthClientMetadataDocument covers the inline Client ID Metadata Document:
// the document's URL is the client_id, its redirect URI is used for the
// authorization and token requests, and no client is registered.
func TestOAuthClientMetadataDocument(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	origin := server.URL
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the document must not register a client: %s %s", r.Method, r.URL.Path)
	})
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.discovery = &oauth.OAuthDiscoveryState{
		AuthorizationServerURL: origin,
		AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
			Issuer: origin, AuthorizationEndpoint: origin + "/authorize", TokenEndpoint: origin + "/token",
			ResponseTypesSupported: []string{"code"},
		},
	}
	provider.clientDocument = &oauth.OAuthClientMetadataDocument{
		URL: "https://pi.dev/oauth/abc/client.json", RedirectURL: "http://127.0.0.1:1455/callback/abc",
	}
	result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{ServerURL: origin + "/mcp"})
	if err != nil || result != oauth.ResultRedirect {
		t.Fatalf("authorize = %s, %v", result, err)
	}
	authorizationURL, err := url.Parse(provider.authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorizationURL.Query().Get("client_id"); got != "https://pi.dev/oauth/abc/client.json" {
		t.Fatalf("client_id = %q", got)
	}
	if got := authorizationURL.Query().Get("redirect_uri"); got != "http://127.0.0.1:1455/callback/abc" {
		t.Fatalf("redirect_uri = %q", got)
	}
	if provider.client != nil {
		t.Fatal("a client document must not be stored")
	}

	// The document URL is validated as a WHATWG URL, while the configured value remains client_id.
	provider.clientDocument = &oauth.OAuthClientMetadataDocument{
		URL: "https:pi.dev/oauth/abc/client.json", RedirectURL: "http://127.0.0.1:1455/callback/abc",
	}
	if result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{ServerURL: origin + "/mcp"}); err != nil || result != oauth.ResultRedirect {
		t.Fatalf("special client document authorize = %s, %v", result, err)
	}
	parsedSpecial, err := url.Parse(provider.authorizationURL)
	if err != nil || parsedSpecial.Query().Get("client_id") != "https:pi.dev/oauth/abc/client.json" {
		t.Fatalf("special client_id = %q, err = %v", parsedSpecial.Query().Get("client_id"), err)
	}

	// A non-https or root-path document URL is rejected.
	for _, invalid := range []string{"http://pi.dev/oauth/client.json", "https://pi.dev/", "https://pi.dev", "https:"} {
		provider.clientDocument = &oauth.OAuthClientMetadataDocument{URL: invalid, RedirectURL: "http://127.0.0.1/callback"}
		if _, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{ServerURL: origin + "/mcp"}); err == nil {
			t.Fatalf("%q must be rejected", invalid)
		}
	}
}

// TestOAuthCallbackExtraPath covers the server-specific callback path: a
// response on an allowed-but-wrong path is rejected.
func TestOAuthCallbackExtraPath(t *testing.T) {
	callback, err := oauth.ListenCallbackServer(context.Background(), oauth.OAuthCallbackServerOptions{
		Port: 0, ExtraPaths: []string{"/callback/abc"},
	})
	if err != nil {
		t.Skipf("loopback callback server unavailable: %v", err)
	}
	defer callback.Close(context.Background())
	base := callback.RedirectURL // http://host:port/callback

	drive := func(path, state string, waitPath string) (oauth.OAuthCallback, error) {
		resultCh := make(chan oauth.OAuthCallback, 1)
		errCh := make(chan error, 1)
		go func() {
			result, err := callback.WaitForCallback(state, waitPath)
			if err != nil {
				errCh <- err
				return
			}
			resultCh <- result
		}()
		awaitCallbackWaiter(t, callback, state)
		requestURL := strings.Replace(base, "/callback", path, 1) + "?code=abc&state=" + state
		response, err := http.Get(requestURL)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		select {
		case result := <-resultCh:
			return result, nil
		case err := <-errCh:
			return oauth.OAuthCallback{}, err
		case <-time.After(5 * time.Second):
			t.Fatal("callback never resolved")
		}
		return oauth.OAuthCallback{}, nil
	}

	// A response on the base path when the waiter expects the server-specific
	// path fails.
	if _, err := drive("/callback", "state-wrong", "/callback/abc"); err == nil || !strings.Contains(err.Error(), "another redirect URI") {
		t.Fatalf("wrong path err = %v", err)
	}
	// The exact server-specific path succeeds.
	result, err := drive("/callback/abc", "state-right", "/callback/abc")
	if err != nil || result.Code != "abc" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

// Reference: pi v1.4.0 (4a080252), packages/mcp/src/oauth/flow.ts runFlow:
// `options.authorizationServerMetadataUrl && secureEndpoint(...)` makes an empty
// string mean "not configured", so cached discovery applies.
func TestOAuthEmptyConfiguredMetadataURLUsesDiscovery(t *testing.T) {
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.client = &oauth.OAuthClientInformationFull{
		ClientID: "client",
	}
	provider.discovery = &oauth.OAuthDiscoveryState{
		AuthorizationServerURL: "https://idp.example",
		AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
			Issuer: "https://idp.example", AuthorizationEndpoint: "https://idp.example/authorize",
			TokenEndpoint: "https://idp.example/token", ResponseTypesSupported: []string{"code"},
			CodeChallengeMethodsSupported: []string{"S256"},
		},
	}
	empty := ""
	result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
		ServerURL: "https://mcp.example/mcp", AuthorizationServerMetadataURL: &empty,
	})
	if err != nil || result != oauth.ResultRedirect {
		t.Fatalf("authorize = %q, %v; want redirect from cached discovery", result, err)
	}
	if provider.authorizationURL == "" {
		t.Fatal("no authorization URL")
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/oauth/flow.ts: a 401 refresh passes
// `refreshOnly: true` (auth-provider.ts:128), and runFlow then returns REDIRECT
// instead of registering a client or preparing an authorization when none is
// stored (`if (!client) { if (options.refreshOnly) return "REDIRECT"; ... }`,
// flow.ts:396). Registering there would replace the client of a sign-in that is
// still pending.
func TestOAuthRefreshOnlyRedirectsWithoutRegisteringWhenNoClientIsStored(t *testing.T) {
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.discovery = &oauth.OAuthDiscoveryState{
		AuthorizationServerURL: "https://idp.example",
		AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
			Issuer: "https://idp.example", AuthorizationEndpoint: "https://idp.example/authorize",
			TokenEndpoint: "https://idp.example/token", ResponseTypesSupported: []string{"code"},
			CodeChallengeMethodsSupported: []string{"S256"},
			RegistrationEndpoint:          new("https://idp.example/register"),
		},
	}
	var fetched []string
	fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
		fetched = append(fetched, request.URL.String())
		return nil, fmt.Errorf("no network in this test")
	}
	result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
		ServerURL:   "https://mcp.example/mcp",
		Fetch:       fetch,
		RefreshOnly: true,
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if result != oauth.ResultRedirect {
		t.Fatalf("result = %q, want REDIRECT", result)
	}
	for _, url := range fetched {
		if strings.Contains(url, "/register") {
			t.Fatalf("a refresh-only flow registered a client: %v", fetched)
		}
	}
	if provider.client != nil {
		t.Fatalf("a refresh-only flow stored client information: %+v", provider.client)
	}
}

// A refresh-only flow never moves on to an authorization request: with a stored
// client and an expired refresh token the reference returns REDIRECT
// (flow.ts:453) instead of starting one, so the pending sign-in's state and code
// verifier stay usable.
func TestOAuthRefreshOnlyDoesNotStartAnAuthorization(t *testing.T) {
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.client = &oauth.OAuthClientInformationFull{ClientID: "client"}
	provider.discovery = &oauth.OAuthDiscoveryState{
		AuthorizationServerURL: "https://idp.example",
		AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
			Issuer: "https://idp.example", AuthorizationEndpoint: "https://idp.example/authorize",
			TokenEndpoint: "https://idp.example/token", ResponseTypesSupported: []string{"code"},
			CodeChallengeMethodsSupported: []string{"S256"},
		},
	}
	provider.tokenSet = &oauth.OAuthTokens{AccessToken: "old", RefreshToken: new("expired")}
	fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
		// The refresh fails with a non-retryable OAuth error, so the flow would
		// otherwise fall through to an authorization.
		return &http.Response{
			StatusCode: 400,
			Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant","error_description":"expired"}`)),
		}, nil
	}
	result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
		ServerURL:   "https://mcp.example/mcp",
		Fetch:       fetch,
		RefreshOnly: true,
	})
	if result != oauth.ResultRedirect {
		t.Fatalf("result = %q (%v), want REDIRECT", result, err)
	}
	if provider.authorizationURL != "" {
		t.Fatalf("a refresh-only flow started an authorization: %s", provider.authorizationURL)
	}
}

// Port of packages/mcp/test/oauth.test.ts "shares one refresh between concurrent
// 401s when refresh tokens rotate", reduced to the invalid_client guard: a
// refresh-only flow compares the *currently* stored client id with the one the
// flow read, and preserves a client a completed sign-in stored meanwhile
// (flow.ts:476-480). Invalidating "all" would discard that sign-in's work.
func TestOAuthRefreshOnlyKeepsAClientStoredByACompletedSignIn(t *testing.T) {
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.client = &oauth.OAuthClientInformationFull{ClientID: "rejected"}
	provider.discovery = &oauth.OAuthDiscoveryState{
		AuthorizationServerURL: "https://idp.example",
		AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
			Issuer: "https://idp.example", AuthorizationEndpoint: "https://idp.example/authorize",
			TokenEndpoint: "https://idp.example/token", ResponseTypesSupported: []string{"code"},
			CodeChallengeMethodsSupported: []string{"S256"},
		},
	}
	provider.verifier = "verifier"
	var calls int
	fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(request.URL.Path, "/token") {
			return nil, fmt.Errorf("unexpected request %s", request.URL)
		}
		calls++
		// The first attempt rejects the client; meanwhile a sign-in stored a new
		// one, so the retry must not wipe it.
		if calls == 1 {
			provider.client = &oauth.OAuthClientInformationFull{ClientID: "signed-in"}
		}
		return &http.Response{
			StatusCode: 400,
			Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_client"}`)),
		}, nil
	}
	_, _ = oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
		ServerURL:         "https://mcp.example/mcp",
		Fetch:             fetch,
		RefreshOnly:       true,
		AuthorizationCode: "code",
	})
	if provider.client == nil || provider.client.ClientID != "signed-in" {
		t.Fatalf("the client stored by the completed sign-in was discarded: %+v", provider.client)
	}
}
