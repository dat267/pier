package oauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
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

// pi v1.4.0 at 4a080252 packages/mcp/src/oauth/discovery.ts defaults each
// authorization-server metadata request to LATEST_PROTOCOL_VERSION when callers
// omit protocolVersion.
func TestDiscoverAuthorizationServerMetadataDefaultsProtocolVersion(t *testing.T) {
	var versions []string
	fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
		versions = append(versions, request.Header.Get("MCP-Protocol-Version"))
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}
	metadata, err := DiscoverAuthorizationServerMetadata(context.Background(), "https://issuer.example", DiscoveryOptions{Fetch: fetch})
	if err != nil || metadata != nil {
		t.Fatalf("metadata = %+v, err = %v; want no discovered metadata", metadata, err)
	}
	if len(versions) != 2 {
		t.Fatalf("protocol version headers = %v, want two candidate requests", versions)
	}
	for _, version := range versions {
		if version != LatestProtocolVersion {
			t.Errorf("MCP-Protocol-Version = %q, want %q", version, LatestProtocolVersion)
		}
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

type stalledDiscoveryBody struct {
	ctx    context.Context
	closed bool
}

func (b *stalledDiscoveryBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stalledDiscoveryBody) Close() error {
	b.closed = true
	return nil
}

// pi v1.4.0 (4a080252), packages/mcp/src/oauth/discovery.ts discard()
// cancels a missing document's body without reading it before the fallback.
func TestDiscoveryDoesNotDrainStalledMissingDocument(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		attempts := 0
		missing := &stalledDiscoveryBody{ctx: ctx}
		fetch := func(ctx context.Context, request *http.Request) (*http.Response, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			attempts++
			if attempts == 1 {
				return &http.Response{StatusCode: http.StatusNotFound, Body: missing}, nil
			}
			if request.URL.Path != "/.well-known/oauth-protected-resource" {
				t.Errorf("fallback path = %q", request.URL.Path)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"resource":"https://fixture.invalid/mcp"}`)),
			}, nil
		}
		metadata, err := DiscoverProtectedResourceMetadata(ctx, "https://fixture.invalid/mcp", DiscoveryOptions{Fetch: fetch})
		if err != nil {
			t.Fatalf("discovery waited for missing document body: %v (attempts=%d)", err, attempts)
		}
		if metadata == nil || metadata.Resource != "https://fixture.invalid/mcp" || attempts != 2 {
			t.Fatalf("metadata=%+v attempts=%d", metadata, attempts)
		}
		if !missing.closed {
			t.Fatal("missing document body not closed")
		}
	})
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/oauth/discovery.ts isDiscoveryMiss: 4xx
// and 502 mean "not here", so discovery tries the next candidate. The boundaries
// matter: 400 is a miss (the port must not fall through on it) and 500 is not.
func TestIsDiscoveryMissBoundaries(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   bool
	}{
		{399, false}, {400, true}, {401, true}, {404, true}, {499, true},
		{500, false}, {501, false}, {502, true}, {503, false}, {200, false},
	} {
		if got := isDiscoveryMiss(tc.status); got != tc.want {
			t.Errorf("isDiscoveryMiss(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// field matches the auth-param name case-insensitively, as the reference's regex
// does with /i (discovery.ts:34), so both an ASCII lowercase letter and its upper
// counterpart must be in the generated character class.
func TestFieldMatchesTheAuthParamNameCaseInsensitively(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   string
	}{
		{`Bearer resource_metadata="https://a.example/m"`, "https://a.example/m"},
		{`Bearer RESOURCE_METADATA="https://a.example/m"`, "https://a.example/m"},
		{`Bearer Resource_Metadata="https://a.example/m"`, "https://a.example/m"},
		{`Bearer resource_metadata=a.example/m, scope="x"`, "a.example/m"},
		{`Bearer resource_metadata=""`, ""},
		{`Bearer scope="x"`, ""},
	} {
		got := ""
		if value := field(tc.header, "resource_metadata"); value != nil {
			got = *value
		}
		if got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.header, got, tc.want)
		}
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/oauth/discovery.ts readMetadata: only a
// 2xx body is parsed, so 299 is accepted and 300 is an HTTP error. The upper
// boundary is off by one in the obvious mutant and would accept a redirect body.
func TestProtectedResourceMetadataAcceptsOnly2xx(t *testing.T) {
	body := `{"resource":"https://a.example/m","authorization_servers":["https://a.example/s"]}`
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{
		{200, false}, {204, false}, {299, false}, {300, true}, {301, true}, {404, true},
	} {
		fetch := func(context.Context, *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		_, err := DiscoverProtectedResourceMetadata(context.Background(), "https://a.example/mcp", DiscoveryOptions{Fetch: fetch})
		if (err != nil) != tc.wantErr {
			t.Errorf("status %d: err = %v, want error = %v", tc.status, err, tc.wantErr)
		}
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/oauth/discovery.ts: a caller that names no
// protocol version gets the latest one on the metadata request, and an explicit
// one is sent verbatim.
func TestDiscoverySendsTheRequestedOrLatestProtocolVersion(t *testing.T) {
	for _, tc := range []struct{ given, want string }{
		{"", LatestProtocolVersion},
		{"2025-06-18", "2025-06-18"},
	} {
		var sent string
		fetch := func(_ context.Context, request *http.Request) (*http.Response, error) {
			sent = request.Header.Get("MCP-Protocol-Version")
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(
				`{"resource":"https://a.example/m","authorization_servers":["https://a.example/s"]}`))}, nil
		}
		if _, err := DiscoverProtectedResourceMetadata(context.Background(), "https://a.example/mcp",
			DiscoveryOptions{Fetch: fetch, ProtocolVersion: tc.given}); err != nil {
			t.Fatalf("%q: %v", tc.given, err)
		}
		if sent != tc.want {
			t.Errorf("version %q sent %q, want %q", tc.given, sent, tc.want)
		}
	}
}
