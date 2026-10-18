package oauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// joinPathForServer mirrors `new URL(path, serverURL)`: the base's userinfo is
// kept, its query and fragment are dropped.
func TestJoinPathForServerDropsQueryAndFragment(t *testing.T) {
	got := joinPathForServer("https://user:pass@example.com:8443/mcp?token=abc#frag", "/authorize")
	want := "https://user:pass@example.com:8443/authorize"
	if got != want {
		t.Fatalf("joinPathForServer = %q, want %q", got, want)
	}
}

// The discovery URLs resolve against the issuer's origin, like
// `new URL(path, issuer.origin)`: userinfo, query and fragment are dropped.
func TestBuildAuthorizationServerDiscoveryUrlsUseOrigin(t *testing.T) {
	urls := BuildAuthorizationServerDiscoveryUrls("https://user:pass@example.com:8443/mcp?token=abc#frag")
	want := []string{
		"https://example.com:8443/.well-known/oauth-authorization-server/mcp",
		"https://example.com:8443/.well-known/openid-configuration/mcp",
		"https://example.com:8443/mcp/.well-known/openid-configuration",
	}
	if len(urls) != len(want) {
		t.Fatalf("urls = %d, want %d", len(urls), len(want))
	}
	for i := range want {
		if urls[i].URL != want[i] {
			t.Errorf("urls[%d] = %q, want %q", i, urls[i].URL, want[i])
		}
	}
}

// The protected-resource request also targets the origin, so the server URL's
// query and userinfo never reach the well-known endpoint.
func TestDiscoverProtectedResourceMetadataUsesOrigin(t *testing.T) {
	var requested string
	fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
		requested = request.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"resource":"https://example.com/mcp"}`)),
		}, nil
	}
	if _, err := DiscoverProtectedResourceMetadata(context.Background(), "https://user:pass@example.com:8443/mcp?token=abc#frag", DiscoveryOptions{Fetch: fetch}); err != nil {
		t.Fatal(err)
	}
	want := "https://example.com:8443/.well-known/oauth-protected-resource/mcp"
	if requested != want {
		t.Fatalf("requested = %q, want %q", requested, want)
	}
}
