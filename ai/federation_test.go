package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Port of the Anthropic SDK's OIDC federation exchange (pi a9424cd43).

func TestRequireSecureTokenEndpoint(t *testing.T) {
	for _, baseURL := range []string{"", "https://api.anthropic.com", "http://127.0.0.1:8080", "http://localhost", "http://[::1]:1"} {
		if err := RequireSecureTokenEndpoint(baseURL); err != nil {
			t.Fatalf("%q: %v", baseURL, err)
		}
	}
	for _, baseURL := range []string{"http://example.com", "ftp://api.anthropic.com", "://bad"} {
		if err := RequireSecureTokenEndpoint(baseURL); err == nil {
			t.Fatalf("%q must be rejected", baseURL)
		}
	}
}

func TestRedactSensitive(t *testing.T) {
	redacted := RedactSensitive(`{"error":"invalid_grant","error_description":"bad","access_token":"secret","assertion":"jwt"}`)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(redacted), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["error"] != "invalid_grant" || decoded["error_description"] != "bad" {
		t.Fatalf("redacted = %s", redacted)
	}
	if _, present := decoded["access_token"]; present {
		t.Fatalf("redacted = %s", redacted)
	}
	// A non-JSON body is truncated.
	long := strings.Repeat("x", 2500)
	truncated := RedactSensitive(long)
	if !strings.Contains(truncated, "more chars>") || len(truncated) > 2050 {
		t.Fatalf("truncated = %q", truncated)
	}
}

func writeIdentityToken(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExchangeFederationToken(t *testing.T) {
	var received map[string]string
	var beta, userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != FederationTokenEndpoint {
			t.Errorf("path = %s", r.URL.Path)
		}
		beta = r.Header.Get("anthropic-beta")
		userAgent = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()
	config := FederationConfig{
		IdentityTokenFile: writeIdentityToken(t, "jwt-assertion"),
		FederationRuleID:  "fdr-1", OrganizationID: "org-1", ServiceAccountID: "svc-1", WorkspaceID: "wrkspc_1",
		BaseURL: server.URL,
		Now:     func() time.Time { return time.Unix(1_000_000, 0) },
	}
	token, err := ExchangeFederationToken(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if token.Token != "tok" || token.ExpiresAtUnix != 1_000_000+3600 {
		t.Fatalf("token = %+v", token)
	}
	if received["grant_type"] != GrantTypeJWTBearer || received["assertion"] != "jwt-assertion" ||
		received["federation_rule_id"] != "fdr-1" || received["organization_id"] != "org-1" ||
		received["service_account_id"] != "svc-1" || received["workspace_id"] != "wrkspc_1" {
		t.Fatalf("body = %+v", received)
	}
	if beta != OAuthAPIBetaHeader+","+FederationBetaHeader || userAgent == "" {
		t.Fatalf("headers = %q, %q", beta, userAgent)
	}
}

func TestExchangeFederationTokenErrors(t *testing.T) {
	// A 401 surfaces the guidance and the request id.
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Request-Id", "req-1")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","access_token":"secret"}`))
	}))
	defer unauthorized.Close()
	config := FederationConfig{IdentityTokenFile: writeIdentityToken(t, "jwt"), FederationRuleID: "r",
		OrganizationID: "o", BaseURL: unauthorized.URL}
	_, err := ExchangeFederationToken(context.Background(), config)
	if err == nil {
		t.Fatal("a 401 must fail")
	}
	var identityError *WorkloadIdentityError
	if !errorsAs(err, &identityError) || identityError.Status != 401 || identityError.RequestID != "req-1" {
		t.Fatalf("err = %+v", err)
	}
	if !strings.Contains(identityError.Message, "Workload identity page") ||
		!strings.Contains(identityError.Message, "ANTHROPIC_WORKSPACE_ID") ||
		strings.Contains(identityError.Message, "secret") {
		t.Fatalf("message = %q", identityError.Message)
	}
	// A missing access_token fails.
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":1}`))
	}))
	defer missing.Close()
	config.BaseURL = missing.URL
	if _, err := ExchangeFederationToken(context.Background(), config); err == nil {
		t.Fatal("a missing access_token must fail")
	}
	// An over-large assertion fails client-side.
	config = FederationConfig{IdentityTokenFile: writeIdentityToken(t, strings.Repeat("x", 17*1024)),
		FederationRuleID: "r", OrganizationID: "o", BaseURL: missing.URL}
	if _, err := ExchangeFederationToken(context.Background(), config); err == nil ||
		!strings.Contains(err.Error(), "assertion limit") {
		t.Fatalf("err = %v", err)
	}
	// A missing identity token file fails.
	config = FederationConfig{IdentityTokenFile: filepath.Join(t.TempDir(), "absent"),
		FederationRuleID: "r", OrganizationID: "o", BaseURL: missing.URL}
	if _, err := ExchangeFederationToken(context.Background(), config); err == nil {
		t.Fatal("a missing identity token file must fail")
	}
}

func TestResolveFederationConfig(t *testing.T) {
	env := ProviderEnv{
		AnthropicFederationRuleIDEnv: "fdr", AnthropicOrganizationIDEnv: "org",
		AnthropicIdentityTokenFileEnv: "/tmp/token", AnthropicServiceAccountIDEnv: "svc",
		AnthropicWorkspaceIDEnv: "wrkspc",
	}
	config, ok := ResolveFederationConfig(env)
	if !ok || config.FederationRuleID != "fdr" || config.OrganizationID != "org" ||
		config.IdentityTokenFile != "/tmp/token" || config.ServiceAccountID != "svc" || config.WorkspaceID != "wrkspc" {
		t.Fatalf("config = %+v, %v", config, ok)
	}
	if _, ok := ResolveFederationConfig(ProviderEnv{AnthropicFederationRuleIDEnv: "fdr"}); ok {
		t.Fatal("the three required variables are needed")
	}
}

func TestFederationTokenCache(t *testing.T) {
	exchanges := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges++
		_, _ = w.Write([]byte(`{"access_token":"tok` + string(rune('0'+exchanges)) + `","expires_in":100}`))
	}))
	defer server.Close()
	now := time.Unix(1_000_000, 0)
	cache := NewFederationTokenCache(FederationConfig{
		IdentityTokenFile: writeIdentityToken(t, "jwt"), FederationRuleID: "r", OrganizationID: "o",
		BaseURL: server.URL, Now: func() time.Time { return now },
	})
	first, err := cache.Token(context.Background())
	if err != nil || first != "tok1" {
		t.Fatalf("token = %q, %v", first, err)
	}
	second, err := cache.Token(context.Background())
	if err != nil || second != "tok1" || exchanges != 1 {
		t.Fatalf("token = %q, exchanges = %d, %v", second, exchanges, err)
	}
	// Past expiry the cache re-exchanges.
	now = now.Add(200 * time.Second)
	third, err := cache.Token(context.Background())
	if err != nil || third != "tok2" || exchanges != 2 {
		t.Fatalf("token = %q, exchanges = %d, %v", third, exchanges, err)
	}
}

func errorsAs(err error, target **WorkloadIdentityError) bool {
	identity, ok := err.(*WorkloadIdentityError)
	if ok {
		*target = identity
	}
	return ok
}

func TestBuildAnthropicRequestFederationBearer(t *testing.T) {
	model := &Model{Provider: "anthropic", ID: "m", BaseURL: "https://api.anthropic.com", API: APIAnthropicMessages}
	request, err := BuildAnthropicRequest(context.Background(), model, &AnthropicMessageCreateParams{},
		anthropicClientOptions{FederationBearer: "fed-token"})
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer fed-token" {
		t.Fatalf("authorization = %q", got)
	}
	if got := request.Header.Get("X-Api-Key"); got != "" {
		t.Fatalf("x-api-key = %q", got)
	}
}

func TestAnthropicFederationBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == FederationTokenEndpoint {
			_, _ = w.Write([]byte(`{"access_token":"fed","expires_in":3600}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	env := ProviderEnv{
		AnthropicFederationRuleIDEnv: "rule-" + t.Name(), AnthropicOrganizationIDEnv: "org",
		AnthropicIdentityTokenFileEnv: writeIdentityToken(t, "jwt"),
	}
	model := &Model{Provider: "anthropic", ID: "m", BaseURL: server.URL, API: APIAnthropicMessages}
	token, present, err := anthropicFederationBearer(context.Background(), model, env)
	if err != nil || !present || token != "fed" {
		t.Fatalf("token = %q, %v, %v", token, present, err)
	}
	// Another provider never uses Anthropic federation.
	if _, present, _ := anthropicFederationBearer(context.Background(), &Model{Provider: "openai"}, env); present {
		t.Fatal("a non-anthropic provider must not federate")
	}
	// No federation env means no token.
	if _, present, _ := anthropicFederationBearer(context.Background(), model, nil); present {
		t.Fatal("no env means no federation")
	}
}
