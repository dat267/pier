package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// M35: successful JSON bodies are untrusted network data. Each endpoint below
// buffers its response before validation, so an oversized body must be rejected
// as a protocol error instead of being held in memory.
const (
	oversizedClassifierBody = 2 << 20
	oversizedCatalogBody    = 17 << 20
	oversizedMetadataToken  = 2 << 20
)

func TestOpenAIDecisionsRejectsOversizedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"answers":[]}`))
		_, _ = w.Write([]byte(strings.Repeat(" ", oversizedClassifierBody)))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	zero := 0
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{APIKey: "secret", MaxRetries: &zero})
	if result.StopReason != ClassifierStopError || result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "size limit") {
		t.Fatalf("oversized response accepted: %+v", result)
	}
}

// Control: the same endpoint still accepts a body inside the limit.
func TestOpenAIDecisionsAcceptsResponseBodyInsideTheLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"answers":[]}`))
		_, _ = w.Write([]byte(strings.Repeat(" ", 1024)))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	zero := 0
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{APIKey: "secret", MaxRetries: &zero})
	if result.StopReason == ClassifierStopError {
		t.Fatalf("in-limit response rejected: %v", *result.ErrorMessage)
	}
}

func TestTypeSafeSystemOneRejectsOversizedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"answers":{}}`))
		_, _ = w.Write([]byte(strings.Repeat(" ", oversizedClassifierBody)))
	}))
	defer server.Close()
	model := &Model{
		ID: "typesafe/jev-1.13", Name: "Jev", API: APITypeSafeSystemOne,
		Provider: "openrouter", BaseURL: server.URL + "/v1", Type: "classifier",
		Input: []string{"text"},
	}
	zero := 0
	result := (TypeSafeSystemOne{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{APIKey: "secret", MaxRetries: &zero})
	if result.StopReason != ClassifierStopError || result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "size limit") {
		t.Fatalf("oversized response accepted: %+v", result)
	}
}

func TestPersonalizeOpenRouterCatalogRejectsOversizedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"m","pricing":{},"ignored":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", oversizedCatalogBody)))
		_, _ = w.Write([]byte(`"}]}`))
	}))
	defer server.Close()
	_, err := PersonalizeOpenRouterCatalog([]*Model{{ID: "m"}}, &RefreshModelsContext{
		Credential: &Credential{Type: CredentialAPIKey, APIKey: &ApiKeyCredential{Key: "key"}},
		BaseURL:    server.URL, Ctx: context.Background(),
	})
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized catalog accepted: err=%v", err)
	}
}

func TestVertexMetadataTokenRejectsOversizedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", oversizedMetadataToken)))
		_, _ = w.Write([]byte(`","expires_in":3600}`))
	}))
	defer server.Close()
	vertexTokenCache.mu.Lock()
	previous := vertexTokenCache.entries
	vertexTokenCache.entries = map[string]cachedVertexToken{}
	vertexTokenCache.mu.Unlock()
	t.Cleanup(func() {
		vertexTokenCache.mu.Lock()
		vertexTokenCache.entries = previous
		vertexTokenCache.mu.Unlock()
	})
	env := ProviderEnv{"GCE_METADATA_HOST": strings.TrimPrefix(server.URL, "http://")}
	if token, err := metadataAccessToken(context.Background(), env); err == nil || token != "" {
		t.Fatalf("oversized metadata token accepted: token=%d bytes err=%v", len(token), err)
	}
}

// TestVertexTokenExpiryKeepsShortLifetimesUsable pins the refresh margin. The
// cache gate reuses a token only while `now` is before its expiry, so a
// one-minute margin applied to a lifetime of a minute or less put the expiry in
// the past and every request refreshed. Google ADC reports 3599 seconds, so the
// short case needs a proxy or a malformed document, but the failure mode is a
// refresh per request rather than a slow token.
func TestVertexTokenExpiryKeepsShortLifetimesUsable(t *testing.T) {
	for _, expiresIn := range []int64{1, 30, 59, 60, 61, 119, 120, 121} {
		if expiry := vertexTokenExpiry(expiresIn); !time.Now().Before(expiry) {
			t.Errorf("expires_in %d produced an already-expired token", expiresIn)
		}
	}
	// A normal lifetime keeps the full one-minute margin.
	if remaining := time.Until(vertexTokenExpiry(3599)); remaining > 59*time.Minute || remaining < 58*time.Minute {
		t.Fatalf("3599-second lifetime left %v, want the full minute of margin", remaining)
	}
	// A very large lifetime stays inside the time.Duration range.
	if expiry := vertexTokenExpiry(1 << 61); expiry.IsZero() {
		t.Fatal("a huge lifetime produced a zero expiry")
	}
}
