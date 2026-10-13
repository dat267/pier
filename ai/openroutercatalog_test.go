package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
