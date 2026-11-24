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

func (p *testProvider) ClientMetadata() oauth.OAuthClientMetadata { return p.clientMetadata }

func (p *testProvider) Tokens(context.Context) (*oauth.OAuthTokens, error) { return p.tokenSet, nil }

func (p *testProvider) SaveTokens(_ context.Context, tokens *oauth.OAuthTokens) error {
	p.tokenSet = tokens
	return nil
}

func readJSONBody(r *http.Request) map[string]any {
	body, _ := io.ReadAll(r.Body)
	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)
	return decoded
}

func mustJSON(value any) string {
	enc, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(enc)
}

func TestOAuthDynamicRegistrationDerivesApplicationType(t *testing.T) {
	applicationTypes := make(chan any, 7)
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
		{"http:localhost:1234/callback", nil, "native"},
		{"http://[::1]/callback", nil, "native"},
		{"com.example.app:/callback", nil, "native"},
		{"https://app.example/callback", nil, "web"},
		{"http://remote.example/callback", nil, "web"},
		{"http://localhost/callback", new("web"), "web"},
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

func TestOAuthRefreshCancellationDoesNotStartAuthorization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	refreshStarted := make(chan struct{})
	var refreshOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshOnce.Do(func() { close(refreshStarted) })
			<-ctx.Done()
		}
	}))
	defer func() {
		cancel()
		server.Close()
	}()
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.client = &oauth.OAuthClientInformationFull{ClientID: "client"}
	provider.tokenSet = &oauth.OAuthTokens{AccessToken: "old", RefreshToken: new("refresh"), TokenType: "Bearer"}
	provider.discovery = &oauth.OAuthDiscoveryState{
		AuthorizationServerURL: server.URL,
		AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
			Issuer: server.URL, AuthorizationEndpoint: server.URL + "/authorize",
			TokenEndpoint: server.URL + "/token", ResponseTypesSupported: []string{"code"},
		},
	}
	type outcome struct {
		result oauth.FlowResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := oauth.AuthorizeMcp(ctx, provider, oauth.FlowOptions{ServerURL: server.URL + "/mcp"})
		finished <- outcome{result: result, err: err}
	}()
	select {
	case <-refreshStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh request never started")
	}
	cancel()
	select {
	case result := <-finished:
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("authorize = %s, %v; want cancellation", result.result, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("authorization did not settle after cancellation")
	}
	if provider.authorizationURL != "" {
		t.Fatalf("authorization URL = %q after canceled refresh", provider.authorizationURL)
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
		ClientMetadata: oauth.OAuthClientMetadata{ClientName: new("test")},
		ClientID:       "client",
		Store:          store,
		OnRedirect:     func(context.Context, string) error { return nil },
	})
	if err := provider.SaveTokens(context.Background(), &oauth.OAuthTokens{
		AccessToken: "a1", RefreshToken: new("r1"), TokenType: "Bearer",
	}); err != nil {
		t.Fatal(err)
	}
	auth := AdaptOAuthProvider(provider)
	unauthorized := func() UnauthorizedInfo { return unauthorizedInfo(401, "Bearer", origin+"/mcp", "a1") }

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := auth.OnUnauthorized(context.Background(), unauthorized()); err != nil {
				t.Errorf("onUnauthorized: %v", err)
			}
		})
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
	provider.client = &oauth.OAuthClientInformationFull{ClientID: "client"}
	provider.tokenSet = &oauth.OAuthTokens{AccessToken: "a1", RefreshToken: new("r1"), TokenType: "Bearer", Scope: new("repo read:org")}
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
		ClientMetadata: oauth.OAuthClientMetadata{ClientName: new("test")},
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
		ClientMetadata: oauth.OAuthClientMetadata{ClientName: new("test")},
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

// flow.ts scope selection uses ||, so an empty joined resource scope falls through to client metadata.
// flow.ts carries a registered client's token_endpoint_auth_method hint into the token request.
func TestOAuthClientAuthHintSurvivesTokenExchange(t *testing.T) {
	provider := newTestProvider("http://127.0.0.1/callback")
	secret := "secret"
	hint := "client_secret_post"
	provider.client = &oauth.OAuthClientInformationFull{
		ClientID: "client", ClientSecret: &secret,
		TokenEndpointAuthMethod: &hint,
	}
	provider.verifier = "verifier"
	provider.discovery = &oauth.OAuthDiscoveryState{
		AuthorizationServerURL: "https://auth.example",
		AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
			Issuer: "https://auth.example", AuthorizationEndpoint: "https://auth.example/authorize",
			TokenEndpoint: "https://auth.example/token", ResponseTypesSupported: []string{"code"},
			TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
		},
	}
	var gotAuthorization, gotSecret string
	fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
		gotAuthorization = request.Header.Get("Authorization")
		body, _ := io.ReadAll(request.Body)
		form, _ := url.ParseQuery(string(body))
		gotSecret = form.Get("client_secret")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"access_token":"access","token_type":"Bearer"}`))}, nil
	}
	result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
		ServerURL: "https://mcp.example/mcp", AuthorizationCode: "code", Fetch: fetch,
	})
	if err != nil || result != oauth.ResultAuthorized {
		t.Fatalf("authorize = %s, %v", result, err)
	}
	if gotAuthorization != "" || gotSecret != secret {
		t.Fatalf("token auth = Authorization %q, client_secret %q; want client_secret_post", gotAuthorization, gotSecret)
	}
}

func TestOAuthEmptyResourceScopesFallThroughToClientMetadata(t *testing.T) {
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.clientMetadata.Scope = new("client-scope")
	provider.client = &oauth.OAuthClientInformationFull{
		ClientID: "client",
	}
	provider.discovery = &oauth.OAuthDiscoveryState{
		AuthorizationServerURL: "https://auth.example",
		AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
			Issuer: "https://auth.example", AuthorizationEndpoint: "https://auth.example/authorize",
			ResponseTypesSupported: []string{"code"},
		},
		ResourceMetadata: &oauth.OAuthProtectedResourceMetadata{
			Resource: "https://mcp.example", ScopesSupported: []string{""},
		},
	}
	result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{ServerURL: "https://mcp.example/mcp"})
	if err != nil || result != oauth.ResultRedirect {
		t.Fatalf("authorize = %s, %v", result, err)
	}
	parsed, err := url.Parse(provider.authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("scope"); got != "client-scope" {
		t.Fatalf("scope = %q; want fallback client scope", got)
	}
}
