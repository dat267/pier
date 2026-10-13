package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPersonalizeOpenRouterCatalogRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	_, err := PersonalizeOpenRouterCatalog([]*Model{{ID: "m"}}, &RefreshModelsContext{
		Credential: &Credential{Type: CredentialAPIKey, APIKey: &ApiKeyCredential{Key: "key"}},
		BaseURL:    server.URL, Ctx: context.Background(),
	})
	if err == nil || err.Error() != "Invalid OpenRouter model list response" {
		t.Fatalf("error = %v, want invalid model-list response", err)
	}
}

func TestPersonalizeOpenRouterCatalogUsesEmptyPricingObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"m","pricing":{}}]}`))
	}))
	defer server.Close()
	model := &Model{ID: "m", Provider: "openrouter", Cost: ModelCost{ModelCostRates: ModelCostRates{Input: 3, Output: 4}}}
	result, err := PersonalizeOpenRouterCatalog([]*Model{model}, &RefreshModelsContext{
		Credential: &Credential{Type: CredentialAPIKey, APIKey: &ApiKeyCredential{Key: "key"}},
		BaseURL:    server.URL, Ctx: context.Background(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Cost.Input != 0 || result[0].Cost.Output != 0 {
		t.Fatalf("personalized empty pricing = %+v", result)
	}
}

func TestPersonalizeOpenRouterCatalogKeepsNonChatAndFallsBackOnAuthErrors(t *testing.T) {
	chat := &Model{ID: "openrouter/auto", Provider: "openrouter", ContextWindow: 1000}
	image := &Model{ID: "openrouter/image", Provider: "openrouter", Type: "image"}
	catalog := []*Model{chat, image}

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()
			result, err := PersonalizeOpenRouterCatalog(catalog, &RefreshModelsContext{
				Credential: &Credential{Type: CredentialAPIKey, APIKey: &ApiKeyCredential{Key: "key"}},
				BaseURL:    server.URL, Ctx: context.Background(),
			})
			if err != nil {
				t.Fatalf("personalize: %v", err)
			}
			if len(result) != 2 || result[0] != chat || result[1] != image {
				t.Fatalf("fallback catalog = %+v", result)
			}
		})
	}
}
