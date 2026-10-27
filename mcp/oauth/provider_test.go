package oauth

import (
	"context"
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
