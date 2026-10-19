package oauth

import (
	"context"
	"testing"
)

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
