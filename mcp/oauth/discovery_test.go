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

// discovery.ts swallows malformed optional protected-resource JSON, then tries authorization-server discovery.
func TestDiscoverOAuthServerInfoSwallowsInvalidOptionalResourceJSON(t *testing.T) {
	var paths []string
	fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		status, body := http.StatusNotFound, ""
		if request.URL.Path == "/resource" {
			status, body = http.StatusOK, "not json"
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	resourceURL := "https://mcp.example/resource"
	info, err := DiscoverOAuthServerInfo(context.Background(), "https://mcp.example/mcp", DiscoveryOptions{
		ResourceMetadataURL: &resourceURL,
		Fetch:               fetch,
	})
	if err != nil || info == nil || info.AuthorizationServerMetadata != nil {
		t.Fatalf("info = %+v, err = %v; want fallback discovery result", info, err)
	}
	if len(paths) < 2 || paths[0] != "/resource" {
		t.Fatalf("requested paths = %v; want resource document then authorization discovery", paths)
	}
}

// discovery.ts treats a bare origin as pathname `/` and does not repeat its root metadata request.
func TestDiscoverProtectedResourceMetadataDoesNotRetryBareOriginPath(t *testing.T) {
	requests := 0
	fetch := func(_ context.Context, _ *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	_, err := DiscoverProtectedResourceMetadata(context.Background(), "https://mcp.example", DiscoveryOptions{Fetch: fetch})
	if err == nil || requests != 1 {
		t.Fatalf("err = %v, requests = %d; want one root lookup and its HTTP error", err, requests)
	}
}

// discovery.ts uses URL.pathname, which preserves escaped separators in issuer paths.
func TestBuildAuthorizationServerDiscoveryUrlsPreservesEscapedPath(t *testing.T) {
	urls := BuildAuthorizationServerDiscoveryUrls("https://issuer.example/tenant%2Fname")
	want := "https://issuer.example/.well-known/oauth-authorization-server/tenant%2Fname"
	if len(urls) == 0 || urls[0].URL != want {
		t.Fatalf("first URL = %v; want %q", urls, want)
	}
}

// SelectResource compares origins: the WHATWG origin lowercases the host and
// drops a default port.
func TestSelectResourceNormalizesOrigin(t *testing.T) {
	metadata := &OAuthProtectedResourceMetadata{Resource: "https://EXAMPLE.com:443/mcp"}
	if _, err := SelectResource("https://example.com/mcp", metadata); err != nil {
		t.Fatalf("SelectResource = %v, want match", err)
	}
	if _, err := SelectResource("https://other.example/mcp", metadata); err == nil {
		t.Fatal("SelectResource accepted a different origin")
	}
}

// A whitespace-only challenge must not panic on the scheme index (upstream's
// trimStart().split(/\s+/, 1)[0] yields "").
func TestSelectResourceDoesNotDecodeEscapedPathSeparators(t *testing.T) {
	metadata := &OAuthProtectedResourceMetadata{Resource: "https://example.com/mcp"}
	if _, err := SelectResource("https://example.com/mcp%2Fadmin", metadata); err == nil {
		t.Fatal("an encoded slash must not make a child path inherit the parent resource")
	}
}

func TestParseWwwAuthenticateWhitespaceOnly(t *testing.T) {
	if got := ParseWwwAuthenticate("   \t  "); got != (OAuthChallenge{}) {
		t.Fatalf("ParseWwwAuthenticate(whitespace) = %+v, want empty", got)
	}
	got := ParseWwwAuthenticate(`Bearer error="insufficient_scope", scope="files:read"`)
	if got.Error == nil || *got.Error != "insufficient_scope" {
		t.Fatalf("error = %v, want insufficient_scope", got.Error)
	}
	if got.Scope == nil || *got.Scope != "files:read" {
		t.Fatalf("scope = %v, want files:read", got.Scope)
	}
	if got := ParseWwwAuthenticate("\uFEFFBearer error=invalid_token"); got.Error == nil || *got.Error != "invalid_token" {
		t.Fatalf("BOM-prefixed challenge = %+v; want parsed error", got)
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
