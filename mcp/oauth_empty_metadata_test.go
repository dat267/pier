package mcp

import (
	"context"
	"testing"

	"github.com/dat267/pier/mcp/oauth"
)

// Reference: pi v1.4.0 (4a080252), packages/mcp/src/oauth/flow.ts runFlow:
// `options.authorizationServerMetadataUrl && secureEndpoint(...)` makes an empty
// string mean "not configured", so cached discovery applies.
func TestOAuthEmptyConfiguredMetadataURLUsesDiscovery(t *testing.T) {
	provider := newTestProvider("http://127.0.0.1/callback")
	provider.client = &oauth.OAuthClientInformationFull{
		ClientID: "client",
	}
	provider.discovery = &oauth.OAuthDiscoveryState{
		AuthorizationServerURL: "https://idp.example",
		AuthorizationServerMetadata: &oauth.AuthorizationServerMetadata{
			Issuer: "https://idp.example", AuthorizationEndpoint: "https://idp.example/authorize",
			TokenEndpoint: "https://idp.example/token", ResponseTypesSupported: []string{"code"},
			CodeChallengeMethodsSupported: []string{"S256"},
		},
	}
	empty := ""
	result, err := oauth.AuthorizeMcp(context.Background(), provider, oauth.FlowOptions{
		ServerURL: "https://mcp.example/mcp", AuthorizationServerMetadataURL: &empty,
	})
	if err != nil || result != oauth.ResultRedirect {
		t.Fatalf("authorize = %q, %v; want redirect from cached discovery", result, err)
	}
	if provider.authorizationURL == "" {
		t.Fatal("no authorization URL")
	}
}
