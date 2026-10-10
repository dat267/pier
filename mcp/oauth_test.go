package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/oauth"
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
			ClientName:              strPtr("pi-mcp-test"),
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: strPtr("none"),
		},
	}
}

func (p *testProvider) RedirectURL() string                       { return p.redirectURL }
func (p *testProvider) ClientMetadata() oauth.OAuthClientMetadata { return p.clientMetadata }
func (p *testProvider) ClientMetadataDocument(*oauth.AuthorizationServerMetadata) *oauth.OAuthClientMetadataDocument {
	return p.clientDocument
}
func (p *testProvider) State(context.Context) (string, error) { return "expected-state", nil }
func (p *testProvider) ClientInformation(context.Context) (*oauth.OAuthClientInformation, error) {
	if p.client == nil {
		return nil, nil
	}
	return &p.client.OAuthClientInformation, nil
}
func (p *testProvider) SaveClientInformation(_ context.Context, information *oauth.OAuthClientInformationFull) error {
	p.client = information
	return nil
}
func (p *testProvider) Tokens(context.Context) (*oauth.OAuthTokens, error) { return p.tokenSet, nil }
func (p *testProvider) SaveTokens(_ context.Context, tokens *oauth.OAuthTokens) error {
	p.tokenSet = tokens
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

func readJSONBody(r *http.Request) map[string]any {
	body, _ := io.ReadAll(r.Body)
	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)
	return decoded
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	if status != 0 {
		w.WriteHeader(status)
	}
	w.Header().Set("content-type", "application/json")
	enc, _ := json.Marshal(payload)
	_, _ = w.Write(enc)
}

func mustJSON(value any) string {
	enc, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(enc)
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
func waitCallbackErrorAfterRequest(t *testing.T, callback *oauth.OAuthCallbackServer, state string, request func() (*http.Response, error)) (error, *http.Response) {
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
		return err, response
	case <-time.After(5 * time.Second):
		t.Fatal("callback never resolved")
	}
	return nil, nil
}

func TestOAuthDynamicRegistrationDerivesApplicationType(t *testing.T) {
	applicationTypes := make(chan any, 6)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata := readJSONBody(r)
		applicationTypes <- metadata["application_type"]
		metadata["client_id"] = "test-client"
		writeJSON(w, http.StatusCreated, metadata)
	}))
	defer server.Close()

	cases := []struct {
		redirectURI     string
		applicationType *string
		want            string
	}{
		{"http://127.0.0.1:1234/callback", nil, "native"},
		{"http://[::1]/callback", nil, "native"},
		{"com.example.app:/callback", nil, "native"},
		{"https://app.example/callback", nil, "web"},
		{"http://remote.example/callback", nil, "web"},
		{"http://localhost/callback", strPtr("web"), "web"},
	}
	for _, testCase := range cases {
		registered, err := oauth.RegisterClient(context.Background(), server.URL, oauth.RegisterClientOptions{
			ClientMetadata: oauth.OAuthClientMetadata{
				RedirectURIs: []string{testCase.redirectURI}, ApplicationType: testCase.applicationType,
			},
		})
		if err != nil {
			t.Fatalf("register redirect %q: %v", testCase.redirectURI, err)
		}
		if registered.ApplicationType == nil || *registered.ApplicationType != testCase.want {
			t.Errorf("registered application_type for %q = %v; want %q", testCase.redirectURI, registered.ApplicationType, testCase.want)
		}
		if got := <-applicationTypes; got != testCase.want {
			t.Errorf("request application_type for %q = %v; want %q", testCase.redirectURI, got, testCase.want)
		}
	}
}

func TestOAuthFullFlow(t *testing.T) {
	var expectedChallenge string
	refreshes := 0
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	origin := server.URL

	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 0, map[string]any{
			"resource":              origin + "/mcp",
			"authorization_servers": []string{origin},
			"scopes_supported":      []string{"org:read"},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 0, map[string]any{
			"issuer":                                origin,
			"authorization_endpoint":                origin + "/authorize",
			"token_endpoint":                        origin + "/token",
			"registration_endpoint":                 origin + "/register",
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"token_endpoint_auth_methods_supported": []string{"none"},
			"code_challenge_methods_supported":      []string{"S256"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		metadata := readJSONBody(r)
		metadata["client_id"] = "test-client"
		metadata["client_secret"] = "" // Empty and null optional fields count as absent (#10266).
		writeJSON(w, http.StatusCreated, metadata)
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		expectedChallenge = query.Get("code_challenge")
		redirect, _ := url.Parse(query.Get("redirect_uri"))
		params := redirect.Query()
		params.Set("code", "test-code")
		params.Set("state", query.Get("state"))
		redirect.RawQuery = params.Encode()
		w.Header().Set("location", redirect.String())
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		params, _ := url.ParseQuery(string(body))
		if params.Get("grant_type") == "refresh_token" {
			refreshes++
			writeJSON(w, 0, map[string]any{
				"access_token": "refreshed-token", "token_type": "Bearer",
				"refresh_token": "", "expires_in": nil,
			})
			return
		}
		digest := sha256.Sum256([]byte(params.Get("code_verifier")))
		challenge := base64.RawURLEncoding.EncodeToString(digest[:])
		if params.Get("code") != "test-code" || challenge != expectedChallenge {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		writeJSON(w, 0, map[string]any{
			"access_token": "first-token", "refresh_token": "refresh-token", "token_type": "Bearer", "scope": "",
		})
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		token := r.Header.Get("Authorization")
		if token != "Bearer first-token" && token != "Bearer refreshed-token" {
			w.Header().Set("www-authenticate", fmt.Sprintf(
				// An empty scope falls through to the resource metadata's
				// scopes_supported.
				`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp", scope=""`, origin))
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized"))
			return
		}
		message := readJSONBody(r)
		id, hasID := message["id"]
		if !hasID || id == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		if message["method"] == "initialize" {
			result = map[string]any{
				"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "oauth-test", "version": "1.0.0"},
			}
		} else {
			result = map[string]any{"tools": []any{map[string]any{"name": "issues", "inputSchema": map[string]any{"type": "object"}}}}
		}
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, mustJSON(id), mustJSON(result))
	})

	callback, err := oauth.ListenCallbackServer(context.Background(), oauth.OAuthCallbackServerOptions{TimeoutMs: 5000})
	if err != nil {
		t.Fatal(err)
	}
	provider := newTestProvider(callback.RedirectURL)
	connectWith := func(t *testing.T) *Client {
		t.Helper()
		openGetStream := false
		client := NewClient(ClientOptions{Name: "oauth-test", Version: "1.0.0"})
		transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
			URL:           origin + "/mcp",
			Headers:       map[string]string{"Authorization": "Bearer caller-supplied-stale-token"},
			AuthProvider:  AdaptOAuthProvider(provider),
			OpenGetStream: &openGetStream,
		})
		if _, err := client.Connect(context.Background(), transport); err != nil {
			t.Fatal(err)
		}
		return client
	}

	// First connect: no tokens, the caller's stale header gets 401, the
	// adapter cannot refresh (no grant), so authorization is required.
	firstClient := NewClient(ClientOptions{Name: "oauth-test", Version: "1.0.0"})
	openGetStream := false
	_, connectErr := firstClient.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL:           origin + "/mcp",
		Headers:       map[string]string{"Authorization": "Bearer caller-supplied-stale-token"},
		AuthProvider:  AdaptOAuthProvider(provider),
		OpenGetStream: &openGetStream,
	}))
	if connectErr == nil {
		t.Fatal("first connect did not require authorization")
	}
	var requiredErr *oauth.McpOAuthAuthorizationRequiredError
	if !errorsAs(connectErr, &requiredErr) {
		t.Fatalf("connect err = %v (%T)", connectErr, connectErr)
	}
	authorizationURL, err := url.Parse(provider.authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorizationURL.Query().Get("scope"); got != "org:read" {
		t.Fatalf("scope = %q (url %s)", got, provider.authorizationURL)
	}
	if got := authorizationURL.Query().Get("resource"); got != origin+"/mcp" {
		t.Fatalf("resource = %q", got)
	}

	// Drive the redirect: the authorization endpoint answers 302 with the
	// code and state on the redirect_uri, which hits the callback server.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	redirectCallback, _ := waitCallbackAfterRequest(t, callback, "expected-state", func() (*http.Response, error) {
		response, err := noRedirect.Get(provider.authorizationURL)
		if err != nil {
			return nil, err
		}
		location := response.Header.Get("location")
		_ = response.Body.Close()
		return http.Get(location)
	})
	result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
		ServerURL:         origin + "/mcp",
		AuthorizationCode: redirectCallback.Code,
	})
	if err != nil || result != oauth.ResultAuthorized {
		t.Fatalf("authorize = %s, %v", result, err)
	}

	client := connectWith(t)
	tools, err := client.ListTools(context.Background(), RequestOptions{})
	if err != nil || len(tools) != 1 || tools[0].Name != "issues" {
		t.Fatalf("tools = %+v err %v", tools, err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Stale the access token; the next connect refreshes.
	provider.tokenSet.AccessToken = "stale-token"
	refreshedClient := connectWith(t)
	// Neither token response names a scope, so the grant has the requested
	// scope.
	tokens := provider.tokenSet
	if tokens.AccessToken != "refreshed-token" || tokens.RefreshToken == nil || *tokens.RefreshToken != "refresh-token" ||
		tokens.Scope == nil || *tokens.Scope != "org:read" {
		t.Fatalf("tokens = %+v", tokens)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d", refreshes)
	}
	if err := refreshedClient.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := callback.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func errorsAs(err error, target any) bool {
	return errors.As(err, target)
}

// unauthorizedInfo builds a 401 UnauthorizedInfo.
func unauthorizedInfo(status int, wwwAuthenticate string, serverURL string, token string) UnauthorizedInfo {
	header := http.Header{}
	if wwwAuthenticate != "" {
		header.Set("www-authenticate", wwwAuthenticate)
	}
	return UnauthorizedInfo{
		Response:  &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(""))},
		ServerURL: serverURL,
		Token:     token,
	}
}

// TestOAuthSharesOneRefreshBetweenConcurrent401s ports "shares one refresh
// between concurrent 401s when refresh tokens rotate".
func TestOAuthSharesOneRefreshBetweenConcurrent401s(t *testing.T) {
	var grantsMu sync.Mutex
	var grants []string
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	origin := server.URL
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		// Invalid resource metadata falls back to the server origin instead
		// of failing discovery.
		writeJSON(w, 0, map[string]any{"resource": origin + "/mcp", "authorization_servers": []string{"not a url"}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 0, map[string]any{
			// Issuer without the trailing slash that URL parsing adds to the
			// fallback server URL.
			"issuer": origin, "authorization_endpoint": origin + "/authorize",
			"token_endpoint": origin + "/token", "response_types_supported": []string{"code"},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		params, _ := url.ParseQuery(string(body))
		refreshToken := params.Get("refresh_token")
		grantsMu.Lock()
		grants = append(grants, refreshToken)
		grantsMu.Unlock()
		if refreshToken != "r1" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		time.Sleep(20 * time.Millisecond)
		writeJSON(w, 0, map[string]any{
			"access_token": "a2", "refresh_token": "r2", "token_type": "Bearer", "expires_in": 3600,
		})
	})

	store := &oauth.MemoryOAuthStateStore{}
	provider := oauth.NewMcpOAuthProvider(oauth.McpOAuthProviderOptions{
		ServerURL:      origin + "/mcp",
		RedirectURL:    "http://127.0.0.1/callback",
		ClientMetadata: oauth.OAuthClientMetadata{ClientName: strPtr("test")},
		ClientID:       "client",
		Store:          store,
		OnRedirect:     func(context.Context, string) error { return nil },
	})
	if err := provider.SaveTokens(context.Background(), &oauth.OAuthTokens{
		AccessToken: "a1", RefreshToken: strPtr("r1"), TokenType: "Bearer",
	}); err != nil {
		t.Fatal(err)
	}
	auth := AdaptOAuthProvider(provider)
	unauthorized := func() UnauthorizedInfo { return unauthorizedInfo(401, "Bearer", origin+"/mcp", "a1") }

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := auth.OnUnauthorized(context.Background(), unauthorized()); err != nil {
				t.Errorf("onUnauthorized: %v", err)
			}
		}()
	}
	wg.Wait()
	// A late 401 for a request that still carried the old token must not
	// refresh again.
	if err := auth.OnUnauthorized(context.Background(), unauthorized()); err != nil {
		t.Fatal(err)
	}
	grantsMu.Lock()
	defer grantsMu.Unlock()
	if len(grants) != 1 || grants[0] != "r1" {
		t.Fatalf("grants = %v", grants)
	}
	token, err := auth.Token(context.Background())
	if err != nil || token != "a2" {
		t.Fatalf("token = %q, %v", token, err)
	}
	state, err := store.Load(context.Background())
	if err != nil || state.Tokens.RefreshToken == nil || *state.Tokens.RefreshToken != "r2" {
		t.Fatalf("state tokens = %+v err %v", state.Tokens, err)
	}
	if state.TokensExpireAt == nil || *state.TokensExpireAt <= time.Now().UnixMilli()+3500000 {
		t.Fatalf("tokensExpireAt = %v", state.TokensExpireAt)
	}
}

// TestOAuthStepUpScope ports "asks for authorization instead of refreshing
// when the server needs more scope".
func TestOAuthStepUpScope(t *testing.T) {
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.client = &oauth.OAuthClientInformationFull{OAuthClientInformation: oauth.OAuthClientInformation{ClientID: "client"}}
	provider.tokenSet = &oauth.OAuthTokens{AccessToken: "a1", RefreshToken: strPtr("r1"), TokenType: "Bearer", Scope: strPtr("repo read:org")}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	origin := server.URL
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 0, map[string]any{
			"issuer": origin, "authorization_endpoint": origin + "/authorize",
			"token_endpoint": origin + "/token", "response_types_supported": []string{"code"},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	auth := AdaptOAuthProvider(provider)
	err := auth.OnUnauthorized(context.Background(), unauthorizedInfo(403, `Bearer error="insufficient_scope", scope="repo admin"`, origin+"/mcp", "a1"))
	if err == nil {
		t.Fatal("step-up did not require authorization")
	}
	var requiredErr *oauth.McpOAuthAuthorizationRequiredError
	if !errorsAs(err, &requiredErr) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	// The challenge may list only the missing scopes; the new grant keeps
	// the old ones too.
	authorizationURL, err := url.Parse(provider.authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorizationURL.Query().Get("scope"); got != "repo read:org admin" {
		t.Fatalf("scope = %q", got)
	}
	// The working grant is kept until the user authorizes the new scope.
	if provider.tokenSet.AccessToken != "a1" {
		t.Fatalf("tokenSet = %+v", provider.tokenSet)
	}
}

// TestOAuthBindsCredentialsToServerURL ports "binds persisted credentials
// to the exact MCP server URL".
func TestOAuthBindsCredentialsToServerURL(t *testing.T) {
	store := &oauth.MemoryOAuthStateStore{}
	first := oauth.NewMcpOAuthProvider(oauth.McpOAuthProviderOptions{
		ServerURL: "https://one.example/mcp", RedirectURL: "http://127.0.0.1/callback",
		ClientMetadata: oauth.OAuthClientMetadata{ClientName: strPtr("test")},
		Store:          store,
		OnRedirect:     func(context.Context, string) error { return nil },
	})
	if err := first.SaveTokens(context.Background(), &oauth.OAuthTokens{AccessToken: "secret", TokenType: "Bearer"}); err != nil {
		t.Fatal(err)
	}
	tokens, err := first.Tokens(context.Background())
	if err != nil || tokens == nil || tokens.AccessToken != "secret" {
		t.Fatalf("first tokens = %+v err %v", tokens, err)
	}
	second := oauth.NewMcpOAuthProvider(oauth.McpOAuthProviderOptions{
		ServerURL: "https://two.example/mcp", RedirectURL: "http://127.0.0.1/callback",
		ClientMetadata: oauth.OAuthClientMetadata{ClientName: strPtr("test")},
		Store:          store,
		OnRedirect:     func(context.Context, string) error { return nil },
	})
	tokens, err = second.Tokens(context.Background())
	if err != nil || tokens != nil {
		t.Fatalf("second tokens = %+v err %v", tokens, err)
	}
}

// TestOAuthRejectsIssuerMismatch ports "rejects authorization metadata
// whose issuer does not match discovery".
func TestOAuthRejectsIssuerMismatch(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	origin := server.URL
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 0, map[string]any{
			"issuer": "https://attacker.example", "authorization_endpoint": origin + "/authorize",
			"token_endpoint": origin + "/token", "response_types_supported": []string{"code"},
		})
	})
	_, err := oauth.DiscoverAuthorizationServerMetadata(context.Background(), origin, oauth.DiscoveryOptions{})
	var mismatchErr *oauth.OAuthIssuerMismatchError
	if !errorsAs(err, &mismatchErr) {
		t.Fatalf("err = %v (%T)", err, err)
	}
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
	provider.client = &oauth.OAuthClientInformationFull{OAuthClientInformation: oauth.OAuthClientInformation{ClientID: "client"}}
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
		body, _ := io.ReadAll(r.Body)
		params, _ := url.ParseQuery(string(body))
		codesMu.Lock()
		codes = append(codes, params.Get("code"))
		codesMu.Unlock()
		writeJSON(w, 0, map[string]any{"access_token": "token", "token_type": "Bearer"})
	})
	exchange := func(code string, iss *string, issParameterSupported bool) (string, error) {
		provider := newTestProvider("http://127.0.0.1/callback")
		provider.client = &oauth.OAuthClientInformationFull{OAuthClientInformation: oauth.OAuthClientInformation{ClientID: "client"}}
		provider.verifier = "verifier"
		provider.discovery = &oauth.OAuthDiscoveryState{
			AuthorizationServerURL: origin,
			AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
				Issuer: origin, AuthorizationEndpoint: origin + "/authorize", TokenEndpoint: origin + "/token",
				ResponseTypesSupported:                     []string{"code"},
				AuthorizationResponseIssParameterSupported: boolPtr(issParameterSupported),
			},
		}
		return oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
			ServerURL: origin + "/mcp", AuthorizationCode: code, Iss: iss,
		})
	}
	attacker := "https://attacker.example"
	if _, err := exchange("other", &attacker, false); err == nil {
		t.Fatal("foreign iss accepted")
	}
	if _, err := exchange("missing", nil, true); err == nil {
		t.Fatal("missing iss accepted when the server promises the parameter")
	}
	if result, err := exchange("matching", &origin, true); err != nil || result != oauth.ResultAuthorized {
		t.Fatalf("matching = %s, %v", result, err)
	}
	// Servers that do not promise the parameter may omit it.
	if result, err := exchange("omitted", nil, false); err != nil || result != oauth.ResultAuthorized {
		t.Fatalf("omitted = %s, %v", result, err)
	}
	codesMu.Lock()
	defer codesMu.Unlock()
	if len(codes) != 2 || codes[0] != "matching" || codes[1] != "omitted" {
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
		deniedErr, failure := waitCallbackErrorAfterRequest(t, callback, "s1", func() (*http.Response, error) {
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
	// A non-https or root-path document URL is rejected.
	for _, invalid := range []string{"http://pi.dev/oauth/client.json", "https://pi.dev/"} {
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
