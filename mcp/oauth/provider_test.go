package oauth

import (
	"context"
	"encoding/json"
	"testing"
)

// pi v1.4.0 (4a080252), oauth/provider.ts defaults redirect_uris with ??,
// not an array-length check: an explicit [] remains empty.
func TestMcpOAuthProviderPreservesEmptyRedirectURIs(t *testing.T) {
	options := McpOAuthProviderOptions{
		ServerURL: "https://example.invalid/mcp", RedirectURL: "http://127.0.0.1/callback",
	}
	control := NewMcpOAuthProvider(options).ClientMetadata().RedirectURIs
	if len(control) != 1 || control[0] != options.RedirectURL {
		t.Fatalf("absent redirect URIs did not receive the default: %v", control)
	}
	options.ClientMetadata.RedirectURIs = []string{}
	got := NewMcpOAuthProvider(options).ClientMetadata().RedirectURIs
	if got == nil || len(got) != 0 {
		t.Fatalf("explicit empty redirect URIs were defaulted: %v", got)
	}
}

// pi v1.4.0 (4a080252), oauth/provider.ts MemoryOAuthStateStore uses
// structuredClone on both save and load, including nested discovery documents.
func TestMemoryOAuthStateStoreIsolatesDiscoveryDocuments(t *testing.T) {
	for _, mutation := range []string{"saved-input", "loaded-result"} {
		t.Run(mutation, func(t *testing.T) {
			registration := "https://auth.example.invalid/register"
			supported := true
			state := &McpOAuthState{
				ServerURL: "https://example.invalid/mcp",
				Discovery: &OAuthDiscoveryState{
					AuthorizationServerMetadata: &AuthorizationServerMetadata{
						AuthorizationEndpoint:             "https://auth.example.invalid/authorize",
						RegistrationEndpoint:              &registration,
						ResponseTypesSupported:            []string{"code"},
						ClientIDMetadataDocumentSupported: &supported,
						Extra:                             map[string]json.RawMessage{"note": json.RawMessage(`"original"`)},
					},
					ResourceMetadata: &OAuthProtectedResourceMetadata{
						Resource:             "https://example.invalid/mcp",
						AuthorizationServers: []string{"https://auth.example.invalid"},
						Extra:                map[string]json.RawMessage{"note": json.RawMessage(`"original"`)},
					},
				},
			}
			store := &MemoryOAuthStateStore{}
			if err := store.Save(context.Background(), state); err != nil {
				t.Fatal(err)
			}
			if mutation == "loaded-result" {
				var err error
				state, err = store.Load(context.Background())
				if err != nil {
					t.Fatal(err)
				}
			}
			auth := state.Discovery.AuthorizationServerMetadata
			auth.AuthorizationEndpoint = "changed"
			*auth.RegistrationEndpoint = "changed"
			auth.ResponseTypesSupported[0] = "changed"
			*auth.ClientIDMetadataDocumentSupported = false
			auth.Extra["note"][1] = 'x'
			resource := state.Discovery.ResourceMetadata
			resource.Resource = "changed"
			resource.AuthorizationServers[0] = "changed"
			resource.Extra["note"][1] = 'x'

			saved, err := store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			auth = saved.Discovery.AuthorizationServerMetadata
			if auth.AuthorizationEndpoint != "https://auth.example.invalid/authorize" ||
				*auth.RegistrationEndpoint != "https://auth.example.invalid/register" ||
				auth.ResponseTypesSupported[0] != "code" || !*auth.ClientIDMetadataDocumentSupported ||
				string(auth.Extra["note"]) != `"original"` {
				t.Error("authorization-server discovery document aliases caller-owned data")
			}
			resource = saved.Discovery.ResourceMetadata
			if resource.Resource != "https://example.invalid/mcp" ||
				resource.AuthorizationServers[0] != "https://auth.example.invalid" ||
				string(resource.Extra["note"]) != `"original"` {
				t.Error("protected-resource discovery document aliases caller-owned data")
			}
		})
	}
}

// provider.ts canonicalizes serverUrl with String(new URL(...)) before binding state.
func TestMcpOAuthProviderBindsStateToCanonicalServerURL(t *testing.T) {
	store := &MemoryOAuthStateStore{}
	options := McpOAuthProviderOptions{
		RedirectURL: "http://127.0.0.1/callback",
		Store:       store,
		OnRedirect:  func(context.Context, string) error { return nil },
	}
	options.ServerURL = "https://EXAMPLE.com:443/mcp"
	first := NewMcpOAuthProvider(options)
	if err := first.SaveTokens(context.Background(), &OAuthTokens{AccessToken: "access"}); err != nil {
		t.Fatal(err)
	}

	options.ServerURL = "https://example.com/mcp"
	second := NewMcpOAuthProvider(options)
	tokens, err := second.Tokens(context.Background())
	if err != nil || tokens == nil || tokens.AccessToken != "access" {
		t.Fatalf("tokens = %+v, err = %v; want canonical URL to share stored state", tokens, err)
	}
}
