package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// M35: successful JSON bodies are untrusted network data. Each endpoint below
// buffers its response before validation, so an oversized body must be rejected
// as a protocol error instead of being held in memory.
const (
	oversizedClassifierBody  = 2 << 20
	oversizedCatalogBody     = 17 << 20
	oversizedMetadataToken   = 2 << 20
	oversizedClassifierLimit = 1 << 20
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
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret", MaxRetries: &zero}})
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
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret", MaxRetries: &zero}})
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
	result := (TypeSafeSystemOne{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret", MaxRetries: &zero}})
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
