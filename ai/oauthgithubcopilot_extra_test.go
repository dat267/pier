package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) auth/oauth/github-copilot.ts builds the
// credential as { ..., enterpriseUrl: enterpriseDomain }: for github.com the
// domain is undefined and JSON.stringify drops the key. parseGitHubCopilotModelCatalog
// returns availableModelIds as an array, so an account without picker models
// stores [] rather than null. Pier wrote "enterpriseUrl": "" and stored null.
func TestGitHubCopilotCredentialExtraFieldsMatchTheReference(t *testing.T) {
	fastDeviceCodeFlows(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/login/device/code":
			_, _ = writer.Write([]byte(`{"device_code":"dev","user_code":"CODE","verification_uri":"https://github.com/login/device","interval":0,"expires_in":900}`))
		case "/login/oauth/access_token":
			_, _ = writer.Write([]byte(`{"access_token":"gho_token"}`))
		case "/copilot_internal/v2/token":
			_, _ = writer.Write([]byte(`{"token":"tid=1;proxy-ep=proxy.individual.githubcopilot.com;","expires_at":1000000}`))
		case "/models":
			_, _ = writer.Write([]byte(`{"data":[]}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	withGitHubDomain(t, server.URL, func(domain string) {
		credential, err := LoginGitHubCopilot(&AuthInteraction{
			Ctx:    context.Background(),
			Prompt: func(AuthPrompt) (string, error) { return "", nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		if raw, ok := credential.Extra[CopilotEnterpriseURLKey]; ok {
			t.Fatalf("github.com login stored enterpriseUrl %s", raw)
		}
		if got := string(credential.Extra[CopilotAvailableModelKey]); got != "[]" {
			t.Fatalf("login availableModelIds = %s", got)
		}

		refreshed, err := RefreshGitHubCopilotToken(context.Background(), "gho_token", "")
		if err != nil {
			t.Fatal(err)
		}
		if raw, ok := refreshed.Extra[CopilotEnterpriseURLKey]; ok {
			t.Fatalf("github.com refresh stored enterpriseUrl %s", raw)
		}
		if got := string(refreshed.Extra[CopilotAvailableModelKey]); got != "[]" {
			t.Fatalf("refresh availableModelIds = %s", got)
		}
	})
}
